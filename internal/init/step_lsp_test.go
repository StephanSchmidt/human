package init

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findPlugin looks up a plugin by binary name in the registry.
func findPlugin(t *testing.T, binary string) LspPlugin {
	t.Helper()
	for _, p := range LspRegistry() {
		if p.Binary == binary {
			return p
		}
	}
	t.Fatalf("plugin with binary %q not found in LspRegistry", binary)
	return LspPlugin{}
}

func TestLspSetupStep_Name(t *testing.T) {
	step := NewLspSetupStep(&mockPrompter{}, newTestInstaller(), &WizardState{})
	assert.Equal(t, "lsp-setup", step.Name())
}

func TestLspSetupStep_NoneSelected(t *testing.T) {
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{},
	}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Nil(t, hints)
	assert.Empty(t, installer.pluginInstallCalls)
}

func TestLspSetupStep_SelectError(t *testing.T) {
	prompter := &mockPrompter{
		selectLspsErr: fmt.Errorf("select error"),
	}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "selecting LSP plugins")
}

func TestLspSetupStep_BinaryAlreadyInstalled(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.installed["gopls"] = true
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Empty(t, hints)
	assert.Contains(t, buf.String(), "gopls is already installed")
	assert.Contains(t, buf.String(), "Installing plugin: gopls@claude-code-lsps")
	assert.Equal(t, []string{"gopls@claude-code-lsps"}, installer.pluginInstallCalls)
	assert.Empty(t, installer.binaryInstallCalls)
}

func TestLspSetupStep_AutoInstallSuccess(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Empty(t, hints)
	assert.Contains(t, buf.String(), "Installing gopls...")
	assert.Contains(t, buf.String(), "gopls installed successfully")
	assert.Equal(t, []string{"gopls@claude-code-lsps"}, installer.pluginInstallCalls)
	assert.Equal(t, []string{"go install golang.org/x/tools/gopls@latest"}, installer.binaryInstallCalls)
}

func TestLspSetupStep_AutoInstallFailure(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.binaryInstallErr = fmt.Errorf("install failed")
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Contains(t, hints[0], "Failed to install gopls")
	assert.Contains(t, hints[0], gopls.InstallHint)
	// Plugin should still be installed even if binary install fails
	assert.Equal(t, []string{"gopls@claude-code-lsps"}, installer.pluginInstallCalls)
}

func TestLspSetupStep_ManualOnly(t *testing.T) {
	jdtls := findPlugin(t, "jdtls") // jdtls has empty InstallCmd
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{jdtls},
	}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Contains(t, hints[0], "Install jdtls manually")
	assert.Contains(t, hints[0], jdtls.InstallHint)
	assert.Empty(t, installer.binaryInstallCalls)
	// Plugin is still installed into Claude
	assert.Equal(t, []string{"jdtls@claude-code-lsps"}, installer.pluginInstallCalls)
}

func TestLspSetupStep_PluginInstallFailure(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.pluginInstallErr = fmt.Errorf("plugin install failed")
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	require.Len(t, hints, 1)
	assert.Contains(t, hints[0], "Failed to install plugin gopls@claude-code-lsps")
	assert.Contains(t, hints[0], "claude plugin install gopls@claude-code-lsps")
	// Should not attempt binary install if plugin install fails
	assert.Empty(t, installer.binaryInstallCalls)
}

func TestLspSetupStep_MarketplaceAddCalled(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.installed["gopls"] = true
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Equal(t, []string{"boostvolt/claude-code-lsps"}, installer.marketplaceCalls)
}

func TestLspSetupStep_MarketplaceErrorContinues(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.installed["gopls"] = true
	installer.marketplaceErr = fmt.Errorf("already exists")
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Empty(t, hints)
	assert.Contains(t, buf.String(), "Marketplace may already be registered")
	// Should still proceed with plugin install
	assert.Equal(t, []string{"gopls@claude-code-lsps"}, installer.pluginInstallCalls)
}

func TestLspSetupStep_MultiplePlugins(t *testing.T) {
	gopls := findPlugin(t, "gopls") // has InstallCmd, binary not installed
	jdtls := findPlugin(t, "jdtls") // manual only
	vtsls := findPlugin(t, "vtsls") // has InstallCmd, binary already installed

	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls, jdtls, vtsls},
	}
	installer := newTestInstaller()
	installer.installed["vtsls"] = true
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)

	// All three plugins installed into Claude
	assert.Equal(t, []string{
		"gopls@claude-code-lsps",
		"jdtls@claude-code-lsps",
		"vtsls@claude-code-lsps",
	}, installer.pluginInstallCalls)

	// gopls binary auto-install attempted
	assert.Contains(t, installer.binaryInstallCalls, "go install golang.org/x/tools/gopls@latest")

	// jdtls manual hint
	var hasJdtlsHint bool
	for _, h := range hints {
		if strings.Contains(h, "jdtls") && strings.Contains(h, "manually") {
			hasJdtlsHint = true
		}
	}
	assert.True(t, hasJdtlsHint, "should have jdtls manual install hint")

	// vtsls already installed, no binary install call for it
	for _, call := range installer.binaryInstallCalls {
		assert.NotContains(t, call, "vtsls")
	}
	assert.Contains(t, buf.String(), "vtsls is already installed")
}

func TestLspRegistry_AllPlugins(t *testing.T) {
	reg := LspRegistry()
	assert.Len(t, reg, 9)

	labels := make([]string, len(reg))
	for i, p := range reg {
		labels[i] = p.Label
	}
	assert.Contains(t, labels, "gopls (Go)")
	assert.Contains(t, labels, "rust-analyzer (Rust)")
	assert.Contains(t, labels, "pyright (Python)")
	assert.Contains(t, labels, "jdtls (Java)")
	assert.Contains(t, labels, "solargraph (Ruby)")
	assert.Contains(t, labels, "omnisharp (C#/.NET)")
	assert.Contains(t, labels, "intelephense (PHP)")
	assert.Contains(t, labels, "vtsls (TypeScript/JS)")
	assert.Contains(t, labels, "bash-language-server (Bash)")

	for _, p := range reg {
		assert.NotEmpty(t, p.PluginID, "PluginID for %s", p.Label)
		assert.NotEmpty(t, p.Binary, "Binary for %s", p.Label)
		assert.NotEmpty(t, p.InstallHint, "InstallHint for %s", p.Label)
	}
}

func TestLspSetupStep_AutoSelectFromStacks(t *testing.T) {
	prompter := &mockPrompter{}
	installer := newTestInstaller()
	installer.installed["gopls"] = true
	installer.installed["pyright-langserver"] = true
	state := &WizardState{
		SelectedStacks: []StackType{
			{Label: "Go", FeatureKey: "ghcr.io/devcontainers/features/go:1"},
			{Label: "Python", FeatureKey: "ghcr.io/devcontainers/features/python:1"},
		},
	}
	step := NewLspSetupStep(prompter, installer, state)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Empty(t, hints)
	// Both plugins should be installed without prompting for selection.
	assert.Equal(t, []string{
		"gopls@claude-code-lsps",
		"pyright@claude-code-lsps",
	}, installer.pluginInstallCalls)
	// SelectLspPlugins should NOT have been called (auto-selected from stacks).
	assert.Empty(t, prompter.selectedLsps)
}

func TestLspSetupStep_FallbackWhenNoStacks(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	state := &WizardState{} // no stacks selected
	step := NewLspSetupStep(prompter, installer, state)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	// Should fall back to manual selection.
	assert.Equal(t, []string{"gopls@claude-code-lsps"}, installer.pluginInstallCalls)
}

func TestLspsForStacks(t *testing.T) {
	stacks := []StackType{
		{Label: "Go", FeatureKey: "ghcr.io/devcontainers/features/go:1"},
		{Label: "Python", FeatureKey: "ghcr.io/devcontainers/features/python:1"},
	}
	result := lspsForStacks(stacks)

	assert.Len(t, result, 2)
	binaries := make([]string, len(result))
	for i, lsp := range result {
		binaries[i] = lsp.Binary
	}
	assert.Contains(t, binaries, "gopls")
	assert.Contains(t, binaries, "pyright-langserver")
}

func TestLspsForStacks_Empty(t *testing.T) {
	result := lspsForStacks(nil)
	assert.Empty(t, result)
}

func TestLspsForStacks_NodeFixedIncludesVtsls(t *testing.T) {
	stacks := []StackType{
		{Label: "Node.js 22 (required by Claude Code)", FeatureKey: "ghcr.io/devcontainers/features/node:1", Fixed: true},
	}
	result := lspsForStacks(stacks)

	require.Len(t, result, 1)
	assert.Equal(t, "vtsls", result[0].Binary)
}

func TestStackToLspBinary_AllMappingsValid(t *testing.T) {
	mapping := StackToLspBinary()
	registry := LspRegistry()
	binaries := make(map[string]bool)
	for _, lsp := range registry {
		binaries[lsp.Binary] = true
	}
	for feature, binary := range mapping {
		assert.True(t, binaries[binary],
			"StackToLspBinary maps %s to %s, but no LspPlugin has Binary=%s", feature, binary, binary)
	}
}

func TestEnableLspTool_SetsEnvVar(t *testing.T) {
	origHomeDir := userHomeDir
	userHomeDir = func() (string, error) { return "/fakehome", nil }
	t.Cleanup(func() { userHomeDir = origHomeDir })

	fw := newMockFileWriter()
	var buf bytes.Buffer

	err := enableLspTool(&buf, fw)

	require.NoError(t, err)

	settingsPath := "/fakehome/.claude/settings.json"
	data, ok := fw.files[settingsPath]
	require.True(t, ok, "settings.json should have been written")
	assert.Contains(t, string(data), `"ENABLE_LSP_TOOL": "1"`)
	assert.Contains(t, buf.String(), "Enabled ENABLE_LSP_TOOL")
}

func TestEnableLspTool_MergesExistingSettings(t *testing.T) {
	origHomeDir := userHomeDir
	userHomeDir = func() (string, error) { return "/fakehome", nil }
	t.Cleanup(func() { userHomeDir = origHomeDir })

	fw := newMockFileWriter()
	settingsPath := "/fakehome/.claude/settings.json"
	fw.files[settingsPath] = []byte(`{"hooks": {"Stop": []}, "env": {"OTHER_VAR": "yes"}}`)
	var buf bytes.Buffer

	err := enableLspTool(&buf, fw)

	require.NoError(t, err)
	data := string(fw.files[settingsPath])
	assert.Contains(t, data, `"ENABLE_LSP_TOOL": "1"`)
	assert.Contains(t, data, `"OTHER_VAR": "yes"`)
	assert.Contains(t, data, `"hooks"`)
}

func TestEnableLspTool_AlreadySet(t *testing.T) {
	origHomeDir := userHomeDir
	userHomeDir = func() (string, error) { return "/fakehome", nil }
	t.Cleanup(func() { userHomeDir = origHomeDir })

	fw := newMockFileWriter()
	settingsPath := "/fakehome/.claude/settings.json"
	fw.files[settingsPath] = []byte(`{"env": {"ENABLE_LSP_TOOL": "1"}}`)
	var buf bytes.Buffer

	err := enableLspTool(&buf, fw)

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "already set")
}

func TestLspSetupStep_EnablesLspTool(t *testing.T) {
	origHomeDir := userHomeDir
	userHomeDir = func() (string, error) { return "/fakehome", nil }
	t.Cleanup(func() { userHomeDir = origHomeDir })

	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		selectedLsps: []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.installed["gopls"] = true
	step := NewLspSetupStep(prompter, installer, &WizardState{})
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Empty(t, hints)

	settingsPath := "/fakehome/.claude/settings.json"
	data, ok := fw.files[settingsPath]
	require.True(t, ok, "settings.json should have been written with ENABLE_LSP_TOOL")
	assert.Contains(t, string(data), `"ENABLE_LSP_TOOL": "1"`)
}

// --- helpers ---

func newTestInstaller() *mockLspInstaller {
	return &mockLspInstaller{
		installed: map[string]bool{},
	}
}
