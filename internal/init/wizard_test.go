package init

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StephanSchmidt/human/internal/claude"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
)

// mockPrompter implements Prompter for testing.
type mockPrompter struct {
	overwrite                bool
	overwriteErr             error
	confirmAddTrackers       bool
	confirmAddTrackersErr    error
	selected                 []ServiceType
	selectErr                error
	instanceValues           []map[string]string
	instanceErr              error
	installAgents            bool
	installErr               error
	promptIdx                int
	confirmDevcontainer      bool
	confirmDevcontainerErr   error
	confirmOverwriteDevcont  bool
	confirmOverwriteDevErr   error
	confirmProxy             bool
	confirmProxyErr          error
	selectedStacks           []StackType
	selectStacksErr          error
}

func (m *mockPrompter) ConfirmOverwrite() (bool, error) {
	return m.overwrite, m.overwriteErr
}

func (m *mockPrompter) ConfirmAddTrackers() (bool, error) {
	return m.confirmAddTrackers, m.confirmAddTrackersErr
}

func (m *mockPrompter) SelectServices(_ []ServiceType) ([]ServiceType, error) {
	return m.selected, m.selectErr
}

func (m *mockPrompter) PromptInstance(_ ServiceType) (map[string]string, error) {
	if m.instanceErr != nil {
		return nil, m.instanceErr
	}
	if m.promptIdx >= len(m.instanceValues) {
		return map[string]string{"name": "work"}, nil
	}
	vals := m.instanceValues[m.promptIdx]
	m.promptIdx++
	return vals, nil
}

func (m *mockPrompter) ConfirmAgentInstall() (bool, error) {
	return m.installAgents, m.installErr
}

func (m *mockPrompter) ConfirmDevcontainer() (bool, error) {
	return m.confirmDevcontainer, m.confirmDevcontainerErr
}

func (m *mockPrompter) ConfirmOverwriteDevcontainer() (bool, error) {
	return m.confirmOverwriteDevcont, m.confirmOverwriteDevErr
}

func (m *mockPrompter) ConfirmProxy() (bool, error) {
	return m.confirmProxy, m.confirmProxyErr
}

func (m *mockPrompter) SelectStacks(_ []StackType) ([]StackType, error) {
	return m.selectedStacks, m.selectStacksErr
}

// mockFileWriter implements claude.FileWriter for testing.
type mockFileWriter struct {
	files map[string][]byte
}

func newMockFileWriter() *mockFileWriter {
	return &mockFileWriter{files: make(map[string][]byte)}
}

func (m *mockFileWriter) MkdirAll(_ string, _ os.FileMode) error { return nil }

func (m *mockFileWriter) WriteFile(name string, data []byte, _ os.FileMode) error {
	m.files[name] = data
	return nil
}

func (m *mockFileWriter) ReadFile(name string) ([]byte, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

// failingFileWriter always fails on WriteFile.
type failingFileWriter struct {
	err error
}

func (f *failingFileWriter) MkdirAll(_ string, _ os.FileMode) error            { return nil }
func (f *failingFileWriter) WriteFile(_ string, _ []byte, _ os.FileMode) error { return f.err }
func (f *failingFileWriter) ReadFile(_ string) ([]byte, error)                 { return nil, os.ErrNotExist }

// mockStep implements WizardStep for orchestrator-level tests.
type mockStep struct {
	name   string
	runFn  func(w io.Writer, fw claude.FileWriter) error
	hints  []string
	called bool
}

func (s *mockStep) Name() string { return s.name }

func (s *mockStep) Run(w io.Writer, fw claude.FileWriter) ([]string, error) {
	s.called = true
	if s.runFn != nil {
		return s.hints, s.runFn(w, fw)
	}
	return s.hints, nil
}

// --- Pure data tests (unchanged) ---

func TestServiceRegistry_AllServices(t *testing.T) {
	reg := ServiceRegistry()
	assert.Len(t, reg, 9)

	labels := make([]string, len(reg))
	for i, s := range reg {
		labels[i] = s.Label
	}
	assert.Contains(t, labels, "Jira")
	assert.Contains(t, labels, "GitHub")
	assert.Contains(t, labels, "GitLab")
	assert.Contains(t, labels, "Linear")
	assert.Contains(t, labels, "Azure DevOps")
	assert.Contains(t, labels, "Shortcut")
	assert.Contains(t, labels, "Notion")
	assert.Contains(t, labels, "Figma")
	assert.Contains(t, labels, "Amplitude")
}

func TestEnvVarName(t *testing.T) {
	tests := []struct {
		prefix, name, suffix, want string
	}{
		{"JIRA", "work", "KEY", "JIRA_WORK_KEY"},
		{"GITHUB", "oss", "TOKEN", "GITHUB_OSS_TOKEN"},
		{"AMPLITUDE", "product", "SECRET", "AMPLITUDE_PRODUCT_SECRET"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, EnvVarName(tt.prefix, tt.name, tt.suffix))
	}
}

func TestGenerateConfig_SingleJira(t *testing.T) {
	instances := []serviceInstance{
		{
			Service: ServiceType{
				Label: "Jira", ConfigKey: "jiras",
				EnvVars: []string{"KEY"}, EnvPrefix: "JIRA",
			},
			Values: map[string]string{
				"name": "work", "url": "https://myorg.atlassian.net",
				"user": "me@example.com", "description": "Product backlog",
			},
		},
	}

	got, err := GenerateConfig(instances)
	require.NoError(t, err)

	assert.Contains(t, got, "jiras:")
	assert.Contains(t, got, "name: work")
	assert.Contains(t, got, "url: https://myorg.atlassian.net")
	assert.Contains(t, got, "user: me@example.com")
	assert.Contains(t, got, `description: "Product backlog"`)
	assert.Contains(t, got, "export JIRA_WORK_KEY=your-key")
}

func TestGenerateConfig_MultipleServices(t *testing.T) {
	instances := []serviceInstance{
		{
			Service: ServiceType{
				Label: "GitHub", ConfigKey: "githubs",
				EnvVars: []string{"TOKEN"}, EnvPrefix: "GITHUB",
			},
			Values: map[string]string{"name": "oss"},
		},
		{
			Service: ServiceType{
				Label: "Notion", ConfigKey: "notions",
				EnvVars: []string{"TOKEN"}, EnvPrefix: "NOTION",
			},
			Values: map[string]string{"name": "docs", "description": "Company docs"},
		},
	}

	got, err := GenerateConfig(instances)
	require.NoError(t, err)

	assert.Contains(t, got, "githubs:")
	assert.Contains(t, got, "notions:")
	// Sections should appear in order.
	assert.Less(t, strings.Index(got, "githubs:"), strings.Index(got, "notions:"))
}

func TestGenerateConfig_AmplitudeMultipleEnvVars(t *testing.T) {
	instances := []serviceInstance{
		{
			Service: ServiceType{
				Label: "Amplitude", ConfigKey: "amplitudes",
				EnvVars: []string{"KEY", "SECRET"}, EnvPrefix: "AMPLITUDE",
			},
			Values: map[string]string{"name": "product", "url": "https://amplitude.com"},
		},
	}

	got, err := GenerateConfig(instances)
	require.NoError(t, err)

	assert.Contains(t, got, "export AMPLITUDE_PRODUCT_KEY=your-key")
	assert.Contains(t, got, "export AMPLITUDE_PRODUCT_SECRET=your-secret")
}

func TestGenerateConfig_AzureDevOpsOrg(t *testing.T) {
	instances := []serviceInstance{
		{
			Service: ServiceType{
				Label: "Azure DevOps", ConfigKey: "azuredevops",
				ExtraFields: []string{"org"},
				EnvVars:     []string{"TOKEN"}, EnvPrefix: "AZURE",
			},
			Values: map[string]string{"name": "work", "org": "mycompany"},
		},
	}

	got, err := GenerateConfig(instances)
	require.NoError(t, err)

	assert.Contains(t, got, "org: mycompany")
}

func TestGenerateConfig_Empty(t *testing.T) {
	got, err := GenerateConfig(nil)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(got))
}

// --- Orchestrator (RunInit) tests ---

func TestRunInit_RunsAllSteps(t *testing.T) {
	step1 := &mockStep{name: "step1"}
	step2 := &mockStep{name: "step2"}
	var buf bytes.Buffer

	err := RunInit(&buf, []WizardStep{step1, step2}, newMockFileWriter())

	require.NoError(t, err)
	assert.True(t, step1.called)
	assert.True(t, step2.called)
	assert.Contains(t, buf.String(), "Done!")
}

func TestRunInit_StopsOnStepError(t *testing.T) {
	step1 := &mockStep{
		name: "failing",
		runFn: func(w io.Writer, fw claude.FileWriter) error {
			return fmt.Errorf("boom")
		},
	}
	step2 := &mockStep{name: "skipped"}
	var buf bytes.Buffer

	err := RunInit(&buf, []WizardStep{step1, step2}, newMockFileWriter())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "wizard step")
	assert.Contains(t, err.Error(), "failing")
	assert.True(t, step1.called)
	assert.False(t, step2.called)
}

func TestRunInit_EmptySteps(t *testing.T) {
	var buf bytes.Buffer

	err := RunInit(&buf, nil, newMockFileWriter())

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Done!")
}

func TestRunInit_CollectsHints(t *testing.T) {
	step1 := &mockStep{name: "s1", hints: []string{"Install foo with: brew install foo"}}
	step2 := &mockStep{name: "s2", hints: []string{"Install bar with: npm install -g bar"}}
	step3 := &mockStep{name: "s3"} // no hints
	var buf bytes.Buffer

	err := RunInit(&buf, []WizardStep{step1, step2, step3}, newMockFileWriter())

	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "Done!")
	assert.Contains(t, output, "Install foo with: brew install foo")
	assert.Contains(t, output, "Install bar with: npm install -g bar")
	// Hints appear after "Done!"
	doneIdx := strings.Index(output, "Done!")
	fooIdx := strings.Index(output, "Install foo")
	barIdx := strings.Index(output, "Install bar")
	assert.Greater(t, fooIdx, doneIdx)
	assert.Greater(t, barIdx, doneIdx)
}

// --- ServicesStep tests ---

func TestServicesStep_AddTrackersDeclined(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{confirmAddTrackers: false}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Skipping tracker configuration.")
	assert.Empty(t, fw.files)
}

func TestServicesStep_AddTrackersAccepted(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	prompter := &mockPrompter{
		confirmAddTrackers: true,
		selected:           []ServiceType{registry[1]},
		instanceValues:     []map[string]string{{"name": "oss"}},
	}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Wrote .humanconfig.yaml")
}

func TestServicesStep_AddTrackersError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{confirmAddTrackersErr: fmt.Errorf("prompt error")}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming tracker setup")
}

func TestServicesStep_NoServicesSelected(t *testing.T) {
	prompter := &mockPrompter{confirmAddTrackers: true, selected: nil}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No services selected")
	assert.Empty(t, fw.files)
}

func TestServicesStep_AbortOnExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".humanconfig.yaml"), []byte("existing"), 0o644))

	prompter := &mockPrompter{overwrite: false}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Aborted")
}

func TestServicesStep_FullFlow(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	jira := registry[0]
	github := registry[1]

	prompter := &mockPrompter{
		confirmAddTrackers: true,
		selected:           []ServiceType{jira, github},
		instanceValues: []map[string]string{
			{"name": "work", "url": "https://work.atlassian.net", "user": "dev@work.com", "description": "Work Jira"},
			{"name": "oss"},
		},
	}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Wrote .humanconfig.yaml")
	assert.Contains(t, buf.String(), "JIRA_WORK_KEY")
	assert.Contains(t, buf.String(), "GITHUB_OSS_TOKEN")

	yaml := string(fw.files[".humanconfig.yaml"])
	assert.Contains(t, yaml, "jiras:")
	assert.Contains(t, yaml, "githubs:")
}

func TestServicesStep_OverwriteError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".humanconfig.yaml"), []byte("existing"), 0o644))

	prompter := &mockPrompter{overwriteErr: fmt.Errorf("input error")}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming overwrite")
}

func TestServicesStep_SelectError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{confirmAddTrackers: true, selectErr: fmt.Errorf("select error")}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "selecting services")
}

func TestServicesStep_InstancePromptError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	prompter := &mockPrompter{
		confirmAddTrackers: true,
		selected:           []ServiceType{registry[0]},
		instanceErr:        fmt.Errorf("prompt error"),
	}
	step := NewServicesStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuring service")
}

func TestServicesStep_WriteError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	prompter := &mockPrompter{
		confirmAddTrackers: true,
		selected:           []ServiceType{registry[1]},
		instanceValues:     []map[string]string{{"name": "oss"}},
	}
	step := NewServicesStep(prompter)
	failFw := &failingFileWriter{err: fmt.Errorf("disk full")}
	var buf bytes.Buffer

	_, err := step.Run(&buf, failFw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing config file")
}

func TestServicesStep_Name(t *testing.T) {
	step := NewServicesStep(&mockPrompter{})
	assert.Equal(t, "services", step.Name())
}

// --- AgentInstallStep tests ---

func TestAgentInstallStep_Declined(t *testing.T) {
	prompter := &mockPrompter{installAgents: false}
	step := NewAgentInstallStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "Installing")
}

func TestAgentInstallStep_Accepted(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{installAgents: true}
	step := NewAgentInstallStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Installing Claude Code integration")
}

func TestAgentInstallStep_PromptError(t *testing.T) {
	prompter := &mockPrompter{installErr: fmt.Errorf("install error")}
	step := NewAgentInstallStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming agent install")
}

func TestAgentInstallStep_Name(t *testing.T) {
	step := NewAgentInstallStep(&mockPrompter{})
	assert.Equal(t, "agent-install", step.Name())
}

// --- DevcontainerStep tests ---

func TestDevcontainerStep_Name(t *testing.T) {
	step := NewDevcontainerStep(&mockPrompter{})
	assert.Equal(t, "devcontainer", step.Name())
}

func TestDevcontainerStep_Declined(t *testing.T) {
	prompter := &mockPrompter{confirmDevcontainer: false}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Empty(t, fw.files)
}

func TestDevcontainerStep_BasicConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{
		confirmDevcontainer: true,
		confirmProxy:        false,
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Wrote .devcontainer/devcontainer.json")
	assert.Contains(t, hints, "  1. Start the daemon:  human daemon start")
	assert.Contains(t, hints, "  2. Start container:  export HUMAN_DAEMON_TOKEN=$(human daemon token) && devcontainer up --workspace-folder .")

	data := string(fw.files[".devcontainer/devcontainer.json"])
	assert.Contains(t, data, "mcr.microsoft.com/devcontainers/base:ubuntu")
	assert.Contains(t, data, "ghcr.io/stephanschmidt/treehouse/human:1")
	assert.Contains(t, data, "HUMAN_DAEMON_ADDR")
	assert.Contains(t, data, "HUMAN_DAEMON_TOKEN")
	assert.Contains(t, data, "HUMAN_CHROME_ADDR")
	assert.Contains(t, data, `"BROWSER": "human-browser"`)
	assert.NotContains(t, data, "capAdd")
	assert.Contains(t, data, "ghcr.io/anthropics/devcontainer-features/claude-code:1")
	assert.Contains(t, data, "human install --agent claude")
	assert.NotContains(t, data, "human-proxy-setup")
	assert.NotContains(t, data, "HUMAN_PROXY_ADDR")
}

func TestDevcontainerStep_WithProxy(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{
		confirmDevcontainer: true,
		confirmProxy:        true,
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)

	data := string(fw.files[".devcontainer/devcontainer.json"])
	assert.Contains(t, data, `"capAdd"`)
	assert.Contains(t, data, "NET_ADMIN")
	assert.Contains(t, data, "sudo human-proxy-setup")
	assert.Contains(t, data, "ghcr.io/anthropics/devcontainer-features/claude-code:1")
	assert.Contains(t, data, "human install --agent claude")
	assert.Contains(t, data, "HUMAN_PROXY_ADDR")
	assert.Contains(t, data, `"proxy": true`)
	assert.Contains(t, data, `"BROWSER": "human-browser"`)
}

func TestDevcontainerStep_OverwriteDeclined_InjectsFeature(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".devcontainer"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".devcontainer/devcontainer.json"), []byte(`{"image":"node:20"}`), 0o644))

	prompter := &mockPrompter{
		confirmDevcontainer:     true,
		confirmOverwriteDevcont: false,
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	fw.files[".devcontainer/devcontainer.json"] = []byte(`{"image":"node:20"}`)
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Added human feature to existing devcontainer config.")
	data := string(fw.files[".devcontainer/devcontainer.json"])
	assert.Contains(t, data, "ghcr.io/stephanschmidt/treehouse/human:1")
	assert.Contains(t, data, "node:20")
}

func TestDevcontainerStep_OverwriteDeclined_FeatureAlreadyPresent(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".devcontainer"), 0o755))
	existing := `{"features":{"ghcr.io/stephanschmidt/treehouse/human:1":{}}}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".devcontainer/devcontainer.json"), []byte(existing), 0o644))

	prompter := &mockPrompter{
		confirmDevcontainer:     true,
		confirmOverwriteDevcont: false,
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	fw.files[".devcontainer/devcontainer.json"] = []byte(existing)
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "human feature already present")
}

func TestDevcontainerStep_OverwriteAccepted(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".devcontainer"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".devcontainer/devcontainer.json"), []byte("{}"), 0o644))

	prompter := &mockPrompter{
		confirmDevcontainer:     true,
		confirmOverwriteDevcont: true,
		confirmProxy:            false,
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Wrote .devcontainer/devcontainer.json")
	assert.NotEmpty(t, fw.files[".devcontainer/devcontainer.json"])
}

func TestDevcontainerStep_PromptError(t *testing.T) {
	prompter := &mockPrompter{confirmDevcontainerErr: fmt.Errorf("prompt failed")}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming devcontainer creation")
}

func TestDevcontainerStep_OverwritePromptError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".devcontainer"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".devcontainer/devcontainer.json"), []byte("{}"), 0o644))

	prompter := &mockPrompter{
		confirmDevcontainer:    true,
		confirmOverwriteDevErr: fmt.Errorf("overwrite error"),
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming devcontainer overwrite")
}

func TestDevcontainerStep_ProxyPromptError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{
		confirmDevcontainer: true,
		confirmProxyErr:     fmt.Errorf("proxy error"),
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming proxy setup")
}

// --- StackRegistry tests ---

func TestStackRegistry_AllStacks(t *testing.T) {
	reg := StackRegistry()
	assert.Len(t, reg, 8)

	labels := make([]string, len(reg))
	for i, s := range reg {
		labels[i] = s.Label
	}
	assert.Contains(t, labels, "Go")
	assert.Contains(t, labels, "Rust")
	assert.Contains(t, labels, "Node.js")
	assert.Contains(t, labels, "Python")
	assert.Contains(t, labels, "Java")
	assert.Contains(t, labels, "Ruby")
	assert.Contains(t, labels, ".NET")
	assert.Contains(t, labels, "PHP")
}

func TestDevcontainerStep_WithStacks(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	reg := StackRegistry()
	prompter := &mockPrompter{
		confirmDevcontainer: true,
		confirmProxy:        false,
		selectedStacks:      []StackType{reg[0], reg[3]}, // Go, Python
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	data := string(fw.files[".devcontainer/devcontainer.json"])
	assert.Contains(t, data, "ghcr.io/devcontainers/features/go:1")
	assert.Contains(t, data, "ghcr.io/devcontainers/features/python:1")
	assert.Contains(t, data, "ghcr.io/stephanschmidt/treehouse/human:1")
}

func TestDevcontainerStep_StacksWithProxy(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	reg := StackRegistry()
	prompter := &mockPrompter{
		confirmDevcontainer: true,
		confirmProxy:        true,
		selectedStacks:      []StackType{reg[1]}, // Rust
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	data := string(fw.files[".devcontainer/devcontainer.json"])
	assert.Contains(t, data, "ghcr.io/devcontainers/features/rust:1")
	assert.Contains(t, data, `"proxy": true`)
	assert.Contains(t, data, "sudo human-proxy-setup")
	assert.Contains(t, data, "NET_ADMIN")
}

func TestDevcontainerStep_SelectStacksError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{
		confirmDevcontainer: true,
		confirmProxy:        false,
		selectStacksErr:     fmt.Errorf("stack select error"),
	}
	step := NewDevcontainerStep(prompter)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "selecting language stacks")
}

// --- Integration: full wizard with real steps ---

func TestRunInit_FullWizardFlow(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	jira := registry[0]
	github := registry[1]

	prompter := &mockPrompter{
		confirmAddTrackers: true,
		selected:           []ServiceType{jira, github},
		instanceValues: []map[string]string{
			{"name": "work", "url": "https://work.atlassian.net", "user": "dev@work.com", "description": "Work Jira"},
			{"name": "oss"},
		},
		confirmDevcontainer: true,
		confirmProxy:        false,
		installAgents:       false,
	}

	steps := []WizardStep{
		NewServicesStep(prompter),
		NewDevcontainerStep(prompter),
		NewAgentInstallStep(prompter),
	}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, steps, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Wrote .humanconfig.yaml")
	assert.Contains(t, buf.String(), "JIRA_WORK_KEY")
	assert.Contains(t, buf.String(), "GITHUB_OSS_TOKEN")
	assert.Contains(t, buf.String(), "Wrote .devcontainer/devcontainer.json")
	assert.Contains(t, buf.String(), "Done!")

	yaml := string(fw.files[".humanconfig.yaml"])
	assert.Contains(t, yaml, "jiras:")
	assert.Contains(t, yaml, "githubs:")

	dcJSON := string(fw.files[".devcontainer/devcontainer.json"])
	assert.Contains(t, dcJSON, "ghcr.io/stephanschmidt/treehouse/human:1")
}

func TestRunInit_FullWizardWithAgentInstall(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	prompter := &mockPrompter{
		confirmAddTrackers:  true,
		selected:            []ServiceType{registry[1]}, // GitHub
		instanceValues:      []map[string]string{{"name": "oss"}},
		confirmDevcontainer: false,
		installAgents:       true,
	}

	steps := []WizardStep{
		NewServicesStep(prompter),
		NewDevcontainerStep(prompter),
		NewAgentInstallStep(prompter),
	}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, steps, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Installing Claude Code integration")
	assert.Contains(t, buf.String(), "Done!")
}
