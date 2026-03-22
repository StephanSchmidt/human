package apiclient

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicAuth(t *testing.T) {
	req, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)
	BasicAuth("user", "pass")(req)
	u, p, ok := req.BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "user", u)
	assert.Equal(t, "pass", p)
}

func TestBearerToken(t *testing.T) {
	req, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)
	BearerToken("tok")(req)
	assert.Equal(t, "Bearer tok", req.Header.Get("Authorization"))
}

func TestHeaderAuth(t *testing.T) {
	req, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)
	HeaderAuth("X-API-Key", "secret")(req)
	assert.Equal(t, "secret", req.Header.Get("X-API-Key"))
}

func TestNoAuth(t *testing.T) {
	req, err := http.NewRequest("GET", "https://example.com", nil)
	require.NoError(t, err)
	NoAuth()(req)
	assert.Empty(t, req.Header.Get("Authorization"))
}
