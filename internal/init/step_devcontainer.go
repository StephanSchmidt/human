package init

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/claude"
)

// DevcontainerPrompter abstracts TUI interactions for the devcontainer step.
type DevcontainerPrompter interface {
	ConfirmDevcontainer() (bool, error)
	ConfirmOverwriteDevcontainer() (bool, error)
	ConfirmProxy() (bool, error)
	SelectStacks(available []StackType) ([]StackType, error)
}

type devcontainerStep struct {
	prompter DevcontainerPrompter
}

// NewDevcontainerStep creates a WizardStep that optionally generates .devcontainer/devcontainer.json.
func NewDevcontainerStep(p DevcontainerPrompter) WizardStep {
	return &devcontainerStep{prompter: p}
}

func (s *devcontainerStep) Name() string { return "devcontainer" }

func (s *devcontainerStep) Run(w io.Writer, fw claude.FileWriter) ([]string, error) {
	create, err := s.prompter.ConfirmDevcontainer()
	if err != nil {
		return nil, errors.WrapWithDetails(err, "confirming devcontainer creation")
	}
	if !create {
		return nil, nil
	}

	if _, err := os.Stat(devcontainerPath); err == nil {
		overwrite, promptErr := s.prompter.ConfirmOverwriteDevcontainer()
		if promptErr != nil {
			return nil, errors.WrapWithDetails(promptErr, "confirming devcontainer overwrite")
		}
		if !overwrite {
			hints, ensureErr := ensureHumanFeature(w, fw)
			if ensureErr != nil {
				return nil, ensureErr
			}
			return hints, nil
		}
	}

	proxy, err := s.prompter.ConfirmProxy()
	if err != nil {
		return nil, errors.WrapWithDetails(err, "confirming proxy setup")
	}

	stacks, err := s.prompter.SelectStacks(StackRegistry())
	if err != nil {
		return nil, errors.WrapWithDetails(err, "selecting language stacks")
	}

	cfg := buildDevcontainerConfig(proxy, stacks)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling devcontainer config")
	}
	data = append(data, '\n')

	if err := fw.MkdirAll(devcontainerDir, 0o755); err != nil {
		return nil, errors.WrapWithDetails(err, "creating .devcontainer directory")
	}
	if err := fw.WriteFile(devcontainerPath, data, 0o644); err != nil {
		return nil, errors.WrapWithDetails(err, "writing devcontainer config",
			"path", devcontainerPath)
	}

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "Wrote %s\n", devcontainerPath)

	hints := []string{
		"Next steps (run on the host before starting the container):",
		"  1. Start the daemon:  human daemon start",
		"  2. Start container:  export HUMAN_DAEMON_TOKEN=$(human daemon token) && devcontainer up --workspace-folder .",
	}
	if _, lookErr := exec.LookPath("devcontainer"); lookErr != nil {
		hints = append(hints, "Install the devcontainer CLI with: npm install -g @devcontainers/cli")
	}

	return hints, nil
}

const devcontainerDir = ".devcontainer"
const devcontainerPath = ".devcontainer/devcontainer.json"

type devcontainerConfig struct {
	Name             string                 `json:"name"`
	Image            string                 `json:"image"`
	Features         map[string]interface{} `json:"features"`
	RunArgs          []string               `json:"runArgs,omitempty"`
	CapAdd           []string               `json:"capAdd,omitempty"`
	ForwardPorts     []int                  `json:"forwardPorts"`
	RemoteEnv        map[string]string      `json:"remoteEnv"`
	PostStartCommand string                 `json:"postStartCommand,omitempty"`
}

const humanFeatureKey = "ghcr.io/stephanschmidt/treehouse/human:1"
const claudeFeatureKey = "ghcr.io/anthropics/devcontainer-features/claude-code:1"
const nodeFeatureKey = "ghcr.io/devcontainers/features/node:1"

// ensureHumanFeature reads an existing devcontainer.json and adds the human
// feature if it is missing. Returns hints if the file was updated.
func ensureHumanFeature(w io.Writer, fw claude.FileWriter) ([]string, error) {
	data, err := fw.ReadFile(devcontainerPath)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "reading existing devcontainer config")
	}

	raw := map[string]interface{}{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.WrapWithDetails(err, "parsing existing devcontainer config")
	}

	features, _ := raw["features"].(map[string]interface{})
	if features != nil {
		if _, ok := features[humanFeatureKey]; ok {
			_, _ = fmt.Fprintln(w, "Keeping existing devcontainer config (human feature already present).")
			return nil, nil
		}
	}

	if features == nil {
		features = map[string]interface{}{}
		raw["features"] = features
	}
	features[humanFeatureKey] = map[string]interface{}{}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, errors.WrapWithDetails(err, "marshalling updated devcontainer config")
	}
	out = append(out, '\n')

	if err := fw.WriteFile(devcontainerPath, out, 0o644); err != nil {
		return nil, errors.WrapWithDetails(err, "writing updated devcontainer config")
	}

	_, _ = fmt.Fprintln(w, "Added human feature to existing devcontainer config.")
	return nil, nil
}

func buildDevcontainerConfig(proxy bool, stacks []StackType) devcontainerConfig {
	featureOpts := map[string]interface{}{}
	if proxy {
		featureOpts["proxy"] = true
	}

	features := map[string]interface{}{
		nodeFeatureKey:   map[string]interface{}{"version": "22"},
		humanFeatureKey:  featureOpts,
		claudeFeatureKey: map[string]interface{}{},
	}
	for _, stack := range stacks {
		if stack.Fixed {
			continue // already added with pinned options above
		}
		features[stack.FeatureKey] = map[string]interface{}{}
	}

	cfg := devcontainerConfig{
		Name:         "human secure container",
		Image:        "mcr.microsoft.com/devcontainers/base:ubuntu",
		Features:     features,
		RunArgs:      []string{"--add-host=host.docker.internal:host-gateway"},
		ForwardPorts: []int{19285, 19286},
		RemoteEnv: map[string]string{ // #nosec G101 -- template reference, not a credential
			"HUMAN_DAEMON_ADDR":  "host.docker.internal:19285",
			"HUMAN_DAEMON_TOKEN": "${localEnv:HUMAN_DAEMON_TOKEN}",
			"HUMAN_CHROME_ADDR":  "host.docker.internal:19286",
			"BROWSER":            "human-browser",
		},
	}

	if proxy {
		cfg.CapAdd = []string{"NET_ADMIN"}
		cfg.RemoteEnv["HUMAN_PROXY_ADDR"] = "host.docker.internal:19287"
		cfg.PostStartCommand = "sudo human-proxy-setup && human install --agent claude && human chrome-bridge"
	} else {
		cfg.PostStartCommand = "human install --agent claude && human chrome-bridge"
	}

	return cfg
}
