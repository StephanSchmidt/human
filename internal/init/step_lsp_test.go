package init

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLspSetupStep_Name(t *testing.T) {
	step := NewLspSetupStep(&mockPrompter{}, newTestInstaller())
	assert.Equal(t, "lsp-setup", step.Name())
}

func TestLspSetupStep_Declined(t *testing.T) {
	prompter := &mockPrompter{confirmLspSetup: false}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Nil(t, hints)
	assert.Empty(t, installer.pluginInstallCalls)
}

func TestLspSetupStep_ConfirmError(t *testing.T) {
	prompter := &mockPrompter{confirmLspSetupErr: fmt.Errorf("prompt error")}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirming LSP setup")
}

func TestLspSetupStep_NoneSelected(t *testing.T) {
	prompter := &mockPrompter{
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{},
	}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	hints, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Nil(t, hints)
	assert.Empty(t, installer.pluginInstallCalls)
}

func TestLspSetupStep_SelectError(t *testing.T) {
	prompter := &mockPrompter{
		confirmLspSetup: true,
		selectLspsErr:   fmt.Errorf("select error"),
	}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "selecting LSP plugins")
}

func TestLspSetupStep_BinaryAlreadyInstalled(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.installed["gopls"] = true
	step := NewLspSetupStep(prompter, installer)
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
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer)
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
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.binaryInstallErr = fmt.Errorf("install failed")
	step := NewLspSetupStep(prompter, installer)
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
	jdtls := LspRegistry()[3] // jdtls has empty InstallCmd
	prompter := &mockPrompter{
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{jdtls},
	}
	installer := newTestInstaller()
	step := NewLspSetupStep(prompter, installer)
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
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.pluginInstallErr = fmt.Errorf("plugin install failed")
	step := NewLspSetupStep(prompter, installer)
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
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.installed["gopls"] = true
	step := NewLspSetupStep(prompter, installer)
	fw := newMockFileWriter()
	var buf bytes.Buffer

	_, err := step.Run(&buf, fw)

	require.NoError(t, err)
	assert.Equal(t, []string{"boostvolt/claude-code-lsps"}, installer.marketplaceCalls)
}

func TestLspSetupStep_MarketplaceErrorContinues(t *testing.T) {
	gopls := LspRegistry()[0]
	prompter := &mockPrompter{
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{gopls},
	}
	installer := newTestInstaller()
	installer.installed["gopls"] = true
	installer.marketplaceErr = fmt.Errorf("already exists")
	step := NewLspSetupStep(prompter, installer)
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
	reg := LspRegistry()
	gopls := reg[0] // has InstallCmd, binary not installed
	jdtls := reg[3] // manual only
	vtsls := reg[7] // has InstallCmd, binary already installed

	prompter := &mockPrompter{
		confirmLspSetup: true,
		selectedLsps:    []LspPlugin{gopls, jdtls, vtsls},
	}
	installer := newTestInstaller()
	installer.installed["vtsls"] = true
	step := NewLspSetupStep(prompter, installer)
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

// --- helpers ---

func newTestInstaller() *mockLspInstaller {
	return &mockLspInstaller{
		installed: map[string]bool{},
	}
}
