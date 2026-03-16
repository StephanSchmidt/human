package oauth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectRedirect(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want *RedirectInfo
	}{
		{
			name: "OAuth URL with localhost redirect",
			url:  "https://accounts.google.com/o/oauth2/auth?redirect_uri=http%3A%2F%2Flocalhost%3A38599%2Fcallback&client_id=abc",
			want: &RedirectInfo{Port: 38599, Path: "/callback"},
		},
		{
			name: "OAuth URL with 127.0.0.1 redirect",
			url:  "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2F127.0.0.1%3A9999%2Foauth&scope=read",
			want: &RedirectInfo{Port: 9999, Path: "/oauth"},
		},
		{
			name: "redirect_uri with no path",
			url:  "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A8080",
			want: &RedirectInfo{Port: 8080, Path: "/"},
		},
		{
			name: "non-OAuth URL without redirect_uri",
			url:  "https://example.com/page?foo=bar",
			want: nil,
		},
		{
			name: "redirect_uri to non-localhost host",
			url:  "https://auth.example.com/authorize?redirect_uri=https%3A%2F%2Fexample.com%2Fcallback",
			want: nil,
		},
		{
			name: "redirect_uri localhost without port",
			url:  "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%2Fcallback",
			want: nil,
		},
		{
			name: "malformed URL",
			url:  "://not-a-url",
			want: nil,
		},
		{
			name: "empty URL",
			url:  "",
			want: nil,
		},
		{
			name: "redirect_uri is malformed",
			url:  "https://auth.example.com/authorize?redirect_uri=://bad",
			want: nil,
		},
		{
			name: "unencoded redirect_uri",
			url:  "https://auth.example.com/authorize?redirect_uri=http://localhost:12345/cb",
			want: &RedirectInfo{Port: 12345, Path: "/cb"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectRedirect(tt.url)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRewriteRedirectPort(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		newPort int
		want    string
	}{
		{
			name:    "rewrites port in redirect_uri",
			url:     "https://auth.example.com/authorize?redirect_uri=http%3A%2F%2Flocalhost%3A43601%2Fcallback&client_id=abc",
			newPort: 52000,
		},
		{
			name:    "preserves other query params",
			url:     "https://auth.example.com/authorize?client_id=abc&redirect_uri=http%3A%2F%2Flocalhost%3A43601%2Fcallback&scope=read",
			newPort: 9999,
		},
		{
			name:    "no redirect_uri returns original",
			url:     "https://example.com/page?foo=bar",
			newPort: 9999,
			want:    "https://example.com/page?foo=bar",
		},
		{
			name:    "malformed URL returns original",
			url:     "://bad",
			newPort: 9999,
			want:    "://bad",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewriteRedirectPort(tt.url, tt.newPort)
			if tt.want != "" {
				assert.Equal(t, tt.want, got)
				return
			}
			// For rewrite cases, verify the redirect_uri now has the new port
			// and the original port is gone.
			info := DetectRedirect(got)
			require.NotNil(t, info, "rewritten URL should still be detectable")
			assert.Equal(t, tt.newPort, info.Port)
			assert.Equal(t, "/callback", info.Path)
		})
	}
}
