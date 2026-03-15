package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPolicy_invalidMode(t *testing.T) {
	_, err := NewPolicy("invalid", []string{"example.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported proxy mode")
}

func TestNewPolicy_skipsEmptyDomains(t *testing.T) {
	p, err := NewPolicy(ModeAllow, []string{"", "  ", "example.com"})
	require.NoError(t, err)
	assert.Len(t, p.matchers, 1)
}

func TestPolicy_allowlistExactMatch(t *testing.T) {
	p, err := NewPolicy(ModeAllow, []string{"github.com", "api.openai.com"})
	require.NoError(t, err)

	assert.True(t, p.Allowed("github.com"))
	assert.True(t, p.Allowed("api.openai.com"))
	assert.False(t, p.Allowed("evil.com"))
	assert.False(t, p.Allowed("sub.github.com"))
}

func TestPolicy_allowlistWildcard(t *testing.T) {
	p, err := NewPolicy(ModeAllow, []string{"*.github.com"})
	require.NoError(t, err)

	assert.True(t, p.Allowed("api.github.com"))
	assert.True(t, p.Allowed("deep.sub.github.com"))
	assert.False(t, p.Allowed("github.com"), "wildcard should not match bare domain")
	assert.False(t, p.Allowed("evil.com"))
}

func TestPolicy_blocklistExactMatch(t *testing.T) {
	p, err := NewPolicy(ModeBlock, []string{"evil.com"})
	require.NoError(t, err)

	assert.False(t, p.Allowed("evil.com"))
	assert.True(t, p.Allowed("github.com"))
}

func TestPolicy_blocklistWildcard(t *testing.T) {
	p, err := NewPolicy(ModeBlock, []string{"*.evil.com"})
	require.NoError(t, err)

	assert.False(t, p.Allowed("sub.evil.com"))
	assert.True(t, p.Allowed("evil.com"), "wildcard block should not block bare domain")
	assert.True(t, p.Allowed("github.com"))
}

func TestPolicy_caseInsensitive(t *testing.T) {
	p, err := NewPolicy(ModeAllow, []string{"GitHub.Com"})
	require.NoError(t, err)

	assert.True(t, p.Allowed("github.com"))
	assert.True(t, p.Allowed("GITHUB.COM"))
	assert.True(t, p.Allowed("GitHub.Com"))
}

func TestPolicy_emptyHostname(t *testing.T) {
	p, err := NewPolicy(ModeAllow, []string{"github.com"})
	require.NoError(t, err)

	assert.False(t, p.Allowed(""))
}

func TestBlockAllPolicy(t *testing.T) {
	p := BlockAllPolicy()

	assert.False(t, p.Allowed("github.com"))
	assert.False(t, p.Allowed("anything.example.com"))
	assert.False(t, p.Allowed(""))
}

func TestPolicy_mixedExactAndWildcard(t *testing.T) {
	p, err := NewPolicy(ModeAllow, []string{"example.com", "*.example.com"})
	require.NoError(t, err)

	assert.True(t, p.Allowed("example.com"))
	assert.True(t, p.Allowed("sub.example.com"))
	assert.False(t, p.Allowed("other.com"))
}
