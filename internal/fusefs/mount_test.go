//go:build linux

package fusefs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEnvFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"exact .env", ".env", true},
		{"env local", ".env.local", true},
		{"env production", ".env.production", true},
		{"env development", ".env.development", true},
		{"env test", ".env.test", true},
		{"env with path", "config/.env", true},
		{"env local with path", "config/.env.local", true},
		{"not env", "config.json", false},
		{"not env dotfile", ".gitignore", false},
		{"environment", ".environment", false},
		{"env prefix no dot", "env.local", false},
		{"go file", "main.go", false},
		{"empty", "", false},
		{"just dot", ".", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsEnvFile(tt.filename))
		})
	}
}

func TestIsSensitiveFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     FileKind
	}{
		// Env files
		{".env", ".env", FileKindEnv},
		{".env.local", ".env.local", FileKindEnv},
		{".env.production", ".env.production", FileKindEnv},
		{"nested .env", "config/.env", FileKindEnv},

		// KEY=VALUE config files
		{".npmrc", ".npmrc", FileKindEnv},
		{".pypirc", ".pypirc", FileKindEnv},

		// JSON secret files
		{"credentials.json", "credentials.json", FileKindJSON},
		{"service-account.json", "service-account.json", FileKindJSON},
		{"secrets.json", "secrets.json", FileKindJSON},
		{"nested credentials", "config/credentials.json", FileKindJSON},

		// YAML secret files
		{"secrets.yml", "secrets.yml", FileKindYAML},
		{"secrets.yaml", "secrets.yaml", FileKindYAML},

		// Opaque/binary
		{"pem file", "server.pem", FileKindOpaque},
		{"key file", "server.key", FileKindOpaque},
		{"p12 file", "cert.p12", FileKindOpaque},
		{"pfx file", "cert.pfx", FileKindOpaque},
		{"nested pem", "certs/ca.pem", FileKindOpaque},

		// Non-sensitive
		{"regular file", "main.go", FileKindNone},
		{"gitignore", ".gitignore", FileKindNone},
		{"package.json", "package.json", FileKindNone},
		{"regular json", "config.json", FileKindNone},
		{"regular yaml", "config.yaml", FileKindNone},
		{"empty", "", FileKindNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsSensitiveFile(tt.filename))
		})
	}
}
