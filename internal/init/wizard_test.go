package init

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPrompter implements Prompter for testing.
type mockPrompter struct {
	overwrite      bool
	overwriteErr   error
	selected       []ServiceType
	selectErr      error
	instanceValues []map[string]string
	instanceErr    error
	installAgents  bool
	installErr     error
	promptIdx      int
}

func (m *mockPrompter) ConfirmOverwrite() (bool, error) {
	return m.overwrite, m.overwriteErr
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

func TestRunInit_NoServicesSelected(t *testing.T) {
	prompter := &mockPrompter{selected: nil}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No services selected")
	assert.Empty(t, fw.files)
}

func TestRunInit_AbortOnExistingConfig(t *testing.T) {
	// Create a temp dir with an existing config to trigger the overwrite prompt.
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".humanconfig.yaml"), []byte("existing"), 0o644))

	prompter := &mockPrompter{overwrite: false}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Aborted")
}

func TestRunInit_FullFlow(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	jira := registry[0]
	github := registry[1]

	prompter := &mockPrompter{
		selected: []ServiceType{jira, github},
		instanceValues: []map[string]string{
			{"name": "work", "url": "https://work.atlassian.net", "user": "dev@work.com", "description": "Work Jira"},
			{"name": "oss"},
		},
		installAgents: false,
	}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Wrote .humanconfig.yaml")
	assert.Contains(t, buf.String(), "JIRA_WORK_KEY")
	assert.Contains(t, buf.String(), "GITHUB_OSS_TOKEN")

	yaml := string(fw.files[".humanconfig.yaml"])
	assert.Contains(t, yaml, "jiras:")
	assert.Contains(t, yaml, "githubs:")
}

func TestRunInit_WithAgentInstall(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	prompter := &mockPrompter{
		selected:       []ServiceType{registry[1]}, // GitHub
		instanceValues: []map[string]string{{"name": "oss"}},
		installAgents:  true,
	}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Installing Claude Code integration")
}

func TestRunInit_OverwriteError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".humanconfig.yaml"), []byte("existing"), 0o644))

	prompter := &mockPrompter{overwriteErr: fmt.Errorf("input error")}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming overwrite")
}

func TestRunInit_SelectError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	prompter := &mockPrompter{selectErr: fmt.Errorf("select error")}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "selecting services")
}

func TestRunInit_InstancePromptError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	prompter := &mockPrompter{
		selected:    []ServiceType{registry[0]},
		instanceErr: fmt.Errorf("prompt error"),
	}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuring service")
}

func TestRunInit_WriteError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	prompter := &mockPrompter{
		selected:       []ServiceType{registry[1]},
		instanceValues: []map[string]string{{"name": "oss"}},
	}
	fw := &mockFileWriter{files: make(map[string][]byte)}
	// Override WriteFile to fail.
	failFw := &failingFileWriter{err: fmt.Errorf("disk full")}
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, failFw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing config file")
	_ = fw // appease unused
}

func TestRunInit_AgentInstallError(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	require.NoError(t, os.Chdir(tmpDir))

	registry := ServiceRegistry()
	prompter := &mockPrompter{
		selected:       []ServiceType{registry[1]},
		instanceValues: []map[string]string{{"name": "oss"}},
		installErr:     fmt.Errorf("install error"),
	}
	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := RunInit(&buf, prompter, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming agent install")
}

// failingFileWriter always fails on WriteFile.
type failingFileWriter struct {
	err error
}

func (f *failingFileWriter) MkdirAll(_ string, _ os.FileMode) error             { return nil }
func (f *failingFileWriter) WriteFile(_ string, _ []byte, _ os.FileMode) error   { return f.err }
func (f *failingFileWriter) ReadFile(_ string) ([]byte, error)                   { return nil, os.ErrNotExist }
