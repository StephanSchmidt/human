package cmddaemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gethuman-sh/human/internal/daemon"
	"github.com/gethuman-sh/human/internal/proxy"
)

// writeProjectConfig creates a project directory carrying the given
// .humanconfig.yaml body and returns its path.
func writeProjectConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".humanconfig.yaml"), []byte(body), 0o600))
	return dir
}

// registryFor builds a project registry over dirs, as the daemon does from
// its --project flags.
func registryFor(t *testing.T, dirs ...string) *daemon.ProjectRegistry {
	t.Helper()
	reg, err := daemon.NewProjectRegistry(dirs)
	require.NoError(t, err)
	return reg
}

const allowlistConfig = `project: proxytest
proxy:
  mode: allowlist
  domains:
    - "*.anthropic.com"
    - github.com
`

// The regression the bug is about: the daemon's cwd is "/" when the desktop app
// launches it, and the proxy policy must come from the registered project
// anyway. Chdir into an empty directory so this fails against the old
// LoadConfig(".") code (SC-4819).
func TestBuildProxyServer_policyComesFromProjectNotWorkingDir(t *testing.T) {
	dir := writeProjectConfig(t, allowlistConfig)
	t.Chdir(t.TempDir())

	srv, status, err := buildProxyServer(registryFor(t, dir), "127.0.0.1:0", false, zerolog.Nop(), nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, srv)

	assert.True(t, srv.Policy.Allowed("api.anthropic.com"), "the model API must pass the project's allowlist")
	assert.True(t, srv.Policy.Allowed("github.com"))
	assert.False(t, srv.Policy.Allowed("evil.example.com"))
	assert.Empty(t, status, "a resolved policy needs no banner line")
}

func TestResolveProxyPolicy_readsTheRegisteredProject(t *testing.T) {
	dir := writeProjectConfig(t, allowlistConfig)
	t.Chdir(t.TempDir())

	resolved, err := resolveProxyPolicy(registryFor(t, dir))
	require.NoError(t, err)
	assert.Equal(t, dir, resolved.Dir)
	assert.Empty(t, resolved.BlockAllReason)
	require.NotNil(t, resolved.Config)
	assert.Equal(t, proxy.ModeAllow, resolved.Config.Mode)
	assert.True(t, resolved.Decider.Allowed("api.anthropic.com"))
}

// A malformed config used to be swallowed by `proxyCfg, _ :=` and degrade to
// block-all, so a YAML typo presented as a TLS failure in every container.
func TestBuildProxyServer_malformedConfigFailsLoudly(t *testing.T) {
	dir := writeProjectConfig(t, "proxy:\n  mode: [this is not a string\n")

	srv, _, err := buildProxyServer(registryFor(t, dir), "127.0.0.1:0", false, zerolog.Nop(), nil, nil, nil, nil)
	require.Error(t, err)
	assert.Nil(t, srv)
}

func TestBuildProxyServer_invalidModeFailsLoudly(t *testing.T) {
	dir := writeProjectConfig(t, "proxy:\n  mode: sometimes\n  domains: [github.com]\n")

	_, _, err := buildProxyServer(registryFor(t, dir), "127.0.0.1:0", false, zerolog.Nop(), nil, nil, nil, nil)
	require.Error(t, err)
}

// Block-all must announce itself: the daemon that shipped this bug ran for days
// with an empty matcher set and said nothing anywhere.
func TestBuildProxyServer_noProxySectionAnnouncesBlockAll(t *testing.T) {
	dir := writeProjectConfig(t, "project: noproxy\n")

	srv, status, err := buildProxyServer(registryFor(t, dir), "127.0.0.1:0", false, zerolog.Nop(), nil, nil, nil, nil)
	require.NoError(t, err)
	assert.False(t, srv.Policy.Allowed("api.anthropic.com"))
	assert.Contains(t, status, "blocking all egress")
	assert.Contains(t, status, dir, "the status must name the directory searched")
}

func TestResolveProxyPolicy_noProjectRegistered(t *testing.T) {
	resolved, err := resolveProxyPolicy(nil)
	require.NoError(t, err)
	assert.False(t, resolved.Decider.Allowed("api.anthropic.com"))
	assert.Contains(t, resolved.BlockAllReason, "no project registered")
}

// Option A: several projects resolve to no policy rather than to the union of
// their allowlists, and the reason names them.
func TestResolveProxyPolicy_severalProjectsBlockAllAndSayWhich(t *testing.T) {
	first := writeProjectConfig(t, allowlistConfig)
	second := writeProjectConfig(t, allowlistConfig)

	resolved, err := resolveProxyPolicy(registryFor(t, first, second))
	require.NoError(t, err)
	assert.False(t, resolved.Decider.Allowed("api.anthropic.com"))
	assert.Contains(t, resolved.BlockAllReason, first)
	assert.Contains(t, resolved.BlockAllReason, second)
}

// Interactive mode wraps the policy in a prompt, but the operator must still be
// told no policy was found — the two status lines are independent facts.
func TestBuildProxyServer_interactiveKeepsBlockAllStatus(t *testing.T) {
	dir := writeProjectConfig(t, "project: noproxy\n")

	_, status, err := buildProxyServer(registryFor(t, dir), "127.0.0.1:0", true, zerolog.Nop(), nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, status, "blocking all egress")
	assert.Contains(t, status, "Interactive proxy mode")
}
