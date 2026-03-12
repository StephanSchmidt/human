package browser

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check that DefaultOpener implements Opener.
var _ Opener = DefaultOpener{}

type mockOpener struct {
	openedURL string
	err       error
}

func (m *mockOpener) Open(url string) error {
	m.openedURL = url
	return m.err
}

func TestValidateURL_valid(t *testing.T) {
	tests := []string{
		"https://example.com",
		"http://localhost:8080",
		"https://example.com/path?q=1",
	}
	for _, u := range tests {
		assert.NoError(t, ValidateURL(u), "expected valid: %s", u)
	}
}

func TestValidateURL_empty(t *testing.T) {
	err := ValidateURL("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URL must not be empty")
}

func TestValidateURL_missingScheme(t *testing.T) {
	err := ValidateURL("example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid URL")
}

func TestValidateURL_invalidScheme(t *testing.T) {
	err := ValidateURL("ftp://example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http or https")
}

func TestRunOpen_success(t *testing.T) {
	m := &mockOpener{}
	var buf bytes.Buffer
	err := RunOpen(m, &buf, "https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", m.openedURL)
	assert.Equal(t, "Opened https://example.com\n", buf.String())
}

func TestRunOpen_invalidURL(t *testing.T) {
	m := &mockOpener{}
	var buf bytes.Buffer
	err := RunOpen(m, &buf, "")
	assert.Error(t, err)
	assert.Empty(t, m.openedURL, "opener should not be called for invalid URL")
}

func TestRunOpen_openerError(t *testing.T) {
	m := &mockOpener{err: fmt.Errorf("no browser")}
	var buf bytes.Buffer
	err := RunOpen(m, &buf, "https://example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening browser")
}
