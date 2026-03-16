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
