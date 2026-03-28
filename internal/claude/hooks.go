package claude

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/StephanSchmidt/human/errors"
)

//go:embed embed/human-status-hook.sh
var hookScriptContent []byte

const hookCommand = "bash ~/.claude/hooks/human-status-hook.sh"

// hookEvents lists the Claude Code hook events we register for.
var hookEvents = []struct {
	name    string
	async   bool
	matcher string // "" for default empty matcher; set for events like Notification
}{
	{"UserPromptSubmit", false, ""},  // blocking — must not be async
	{"Stop", true, ""},
	{"SubagentStart", true, ""},
	{"SubagentStop", true, ""},
	{"PermissionRequest", true, ""}, // blocked waiting for tool permission
	{"Notification", true, ".*"},    // catches idle_prompt, permission_prompt, etc.
	{"StopFailure", true, ""},       // API error or crash
	{"SessionStart", true, ""},      // new session began
	{"SessionEnd", true, ""},        // session ended (e.g. /clear)
}

// InstallHooks writes the hook script and registers hooks in ~/.claude/settings.json.
func InstallHooks(w io.Writer, fw FileWriter) error {
	home, err := userHomeDir()
	if err != nil {
		return errors.WrapWithDetails(err, "resolving home directory")
	}

	// Write hook script.
	scriptDir := filepath.Join(home, ".claude", "hooks")
	scriptPath := filepath.Join(scriptDir, "human-status-hook.sh")

	if err := fw.MkdirAll(scriptDir, 0o755); err != nil {
		return errors.WrapWithDetails(err, "creating hooks directory", "path", scriptDir)
	}

	existing, readErr := fw.ReadFile(scriptPath)
	if readErr == nil && string(existing) == string(hookScriptContent) {
		_, _ = fmt.Fprintf(w, "  unchanged %s\n", scriptPath)
	} else {
		action := "created"
		if readErr == nil {
			action = "updated"
		}
		if err := fw.WriteFile(scriptPath, hookScriptContent, 0o755); err != nil {
			return errors.WrapWithDetails(err, "writing hook script", "path", scriptPath)
		}
		_, _ = fmt.Fprintf(w, "  %s %s\n", action, scriptPath)
	}

	// Update settings.json.
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := mergeHooksIntoSettings(w, fw, settingsPath); err != nil {
		return err
	}

	return nil
}

func mergeHooksIntoSettings(w io.Writer, fw FileWriter, path string) error {
	settings := make(map[string]interface{})

	data, err := fw.ReadFile(path)
	if err == nil {
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			return errors.WrapWithDetails(jsonErr, "parsing settings.json", "path", path)
		}
	} else if !os.IsNotExist(err) {
		// ReadFile returned an error that isn't "not found" — could be permission denied.
		// Treat as missing settings and create fresh.
		settings = make(map[string]interface{})
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	changed := false
	for _, evt := range hookEvents {
		if addHookMatcher(hooks, evt.name, evt.async, evt.matcher) {
			changed = true
		}
	}

	if !changed {
		_, _ = fmt.Fprintf(w, "  unchanged %s (hooks already registered)\n", path)
		return nil
	}

	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return errors.WrapWithDetails(err, "marshaling settings.json")
	}
	out = append(out, '\n')

	if err := fw.WriteFile(path, out, 0o644); err != nil {
		return errors.WrapWithDetails(err, "writing settings.json", "path", path)
	}

	_, _ = fmt.Fprintf(w, "  updated %s (hooks registered)\n", path)
	return nil
}

// addHookMatcher adds our hook matcher to an event if not already present.
// Returns true if a new matcher was added.
func addHookMatcher(hooks map[string]interface{}, eventName string, async bool, matcher string) bool {
	matchers, _ := hooks[eventName].([]interface{})

	// Check if our hook already exists.
	for _, m := range matchers {
		matcherObj, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		hookList, _ := matcherObj["hooks"].([]interface{})
		for _, h := range hookList {
			hookDef, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			if cmd, _ := hookDef["command"].(string); cmd == hookCommand {
				return false // already registered
			}
		}
	}

	hookDef := map[string]interface{}{
		"type":    "command",
		"command": hookCommand,
	}
	if async {
		hookDef["async"] = true
	}

	newMatcher := map[string]interface{}{
		"matcher": matcher,
		"hooks":   []interface{}{hookDef},
	}
	hooks[eventName] = append(matchers, newMatcher)
	return true
}
