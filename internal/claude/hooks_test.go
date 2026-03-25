package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallHooks_NewSettings(t *testing.T) {
	fw := newMockFileWriter()
	// ReadFile returns not-found for settings.json → treated as empty.
	fw.readFn = func(name string) ([]byte, error) {
		if filepath.Base(name) == "settings.json" {
			return nil, os.ErrNotExist
		}
		return nil, os.ErrNotExist
	}

	var buf bytes.Buffer
	err := InstallHooks(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "created")
	assert.Contains(t, buf.String(), "hooks registered")

	// Verify settings.json was written with hooks.
	var settingsPath string
	for path := range fw.files {
		if filepath.Base(path) == "settings.json" {
			settingsPath = path
			break
		}
	}
	require.NotEmpty(t, settingsPath, "settings.json should be written")

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(fw.files[settingsPath], &settings))

	hooks, ok := settings["hooks"].(map[string]interface{})
	require.True(t, ok, "hooks key should exist")

	// All 4 events registered.
	for _, evt := range []string{"UserPromptSubmit", "Stop", "SubagentStart", "SubagentStop"} {
		matchers, ok := hooks[evt].([]interface{})
		require.True(t, ok, "event %s should have matchers", evt)
		assert.Len(t, matchers, 1)
	}

	// Verify hook script was written.
	var scriptPath string
	for path := range fw.files {
		if filepath.Base(path) == "human-status-hook.sh" {
			scriptPath = path
			break
		}
	}
	require.NotEmpty(t, scriptPath, "hook script should be written")
	assert.Equal(t, string(hookScriptContent), string(fw.files[scriptPath]))
}

func TestInstallHooks_ExistingSettings(t *testing.T) {
	fw := newMockFileWriter()

	existingSettings := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": []string{"WebSearch"},
		},
		"statusLine": map[string]interface{}{
			"type":    "command",
			"command": "bash ~/status.sh",
		},
	}
	existingJSON, _ := json.Marshal(existingSettings)

	fw.readFn = func(name string) ([]byte, error) {
		if filepath.Base(name) == "settings.json" {
			return existingJSON, nil
		}
		return nil, os.ErrNotExist
	}

	var buf bytes.Buffer
	err := InstallHooks(&buf, fw)

	require.NoError(t, err)

	// Find written settings.json.
	var settingsPath string
	for path := range fw.files {
		if filepath.Base(path) == "settings.json" {
			settingsPath = path
			break
		}
	}
	require.NotEmpty(t, settingsPath)

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(fw.files[settingsPath], &settings))

	// Existing fields preserved.
	perms, ok := settings["permissions"].(map[string]interface{})
	require.True(t, ok, "permissions should be preserved")
	assert.NotNil(t, perms["allow"])

	statusLine, ok := settings["statusLine"].(map[string]interface{})
	require.True(t, ok, "statusLine should be preserved")
	assert.Equal(t, "command", statusLine["type"])

	// Hooks added.
	_, ok = settings["hooks"].(map[string]interface{})
	assert.True(t, ok, "hooks should be added")
}

func TestInstallHooks_Idempotent(t *testing.T) {
	fw := newMockFileWriter()
	fw.readFn = func(name string) ([]byte, error) {
		if filepath.Base(name) == "settings.json" {
			return nil, os.ErrNotExist
		}
		return nil, os.ErrNotExist
	}

	// First install.
	var buf1 bytes.Buffer
	require.NoError(t, InstallHooks(&buf1, fw))

	// Save written settings for second call.
	var settingsPath string
	for path := range fw.files {
		if filepath.Base(path) == "settings.json" {
			settingsPath = path
			break
		}
	}
	firstSettings := fw.files[settingsPath]

	// Second install — reads back what was written.
	fw.readFn = func(name string) ([]byte, error) {
		if filepath.Base(name) == "settings.json" {
			return firstSettings, nil
		}
		if data, ok := fw.files[name]; ok {
			return data, nil
		}
		return nil, os.ErrNotExist
	}

	var buf2 bytes.Buffer
	require.NoError(t, InstallHooks(&buf2, fw))

	assert.Contains(t, buf2.String(), "hooks already registered")

	// Settings should not have duplicate matchers.
	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(fw.files[settingsPath], &settings))
	hooks := settings["hooks"].(map[string]interface{})
	for _, evt := range []string{"UserPromptSubmit", "Stop", "SubagentStart", "SubagentStop"} {
		matchers := hooks[evt].([]interface{})
		assert.Len(t, matchers, 1, "event %s should still have exactly 1 matcher", evt)
	}
}

func TestInstallHooks_MergesWithUserHooks(t *testing.T) {
	fw := newMockFileWriter()

	// User already has a Stop hook.
	existingSettings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"Stop": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "echo user hook",
						},
					},
				},
			},
		},
	}
	existingJSON, _ := json.Marshal(existingSettings)

	fw.readFn = func(name string) ([]byte, error) {
		if filepath.Base(name) == "settings.json" {
			return existingJSON, nil
		}
		return nil, os.ErrNotExist
	}

	var buf bytes.Buffer
	require.NoError(t, InstallHooks(&buf, fw))

	var settingsPath string
	for path := range fw.files {
		if filepath.Base(path) == "settings.json" {
			settingsPath = path
			break
		}
	}

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(fw.files[settingsPath], &settings))
	hooks := settings["hooks"].(map[string]interface{})

	// Stop should have 2 matchers: user's + ours.
	stopMatchers := hooks["Stop"].([]interface{})
	assert.Len(t, stopMatchers, 2, "Stop should have user hook + our hook")

	// UserPromptSubmit should have 1 (only ours).
	promptMatchers := hooks["UserPromptSubmit"].([]interface{})
	assert.Len(t, promptMatchers, 1)
}

func TestInstallHooks_WritesScript(t *testing.T) {
	fw := newMockFileWriter()
	fw.readFn = func(_ string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	var buf bytes.Buffer
	require.NoError(t, InstallHooks(&buf, fw))

	var scriptPath string
	for path := range fw.files {
		if filepath.Base(path) == "human-status-hook.sh" {
			scriptPath = path
			break
		}
	}
	require.NotEmpty(t, scriptPath)

	content := string(fw.files[scriptPath])
	assert.Contains(t, content, "#!/bin/bash")
	assert.Contains(t, content, "hook_event_name")
	assert.Contains(t, content, "human-events")
	assert.Contains(t, content, "events.jsonl")
}

func TestInstallHooks_UserPromptSubmitNotAsync(t *testing.T) {
	fw := newMockFileWriter()
	fw.readFn = func(_ string) ([]byte, error) {
		return nil, os.ErrNotExist
	}

	var buf bytes.Buffer
	require.NoError(t, InstallHooks(&buf, fw))

	var settingsPath string
	for path := range fw.files {
		if filepath.Base(path) == "settings.json" {
			settingsPath = path
			break
		}
	}

	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(fw.files[settingsPath], &settings))
	hooks := settings["hooks"].(map[string]interface{})

	// UserPromptSubmit should NOT have async.
	matchers := hooks["UserPromptSubmit"].([]interface{})
	matcher := matchers[0].(map[string]interface{})
	hookList := matcher["hooks"].([]interface{})
	hookDef := hookList[0].(map[string]interface{})
	_, hasAsync := hookDef["async"]
	assert.False(t, hasAsync, "UserPromptSubmit hook should not have async field")

	// Stop SHOULD have async: true.
	stopMatchers := hooks["Stop"].([]interface{})
	stopMatcher := stopMatchers[0].(map[string]interface{})
	stopHookList := stopMatcher["hooks"].([]interface{})
	stopHookDef := stopHookList[0].(map[string]interface{})
	assert.Equal(t, true, stopHookDef["async"], "Stop hook should have async: true")
}
