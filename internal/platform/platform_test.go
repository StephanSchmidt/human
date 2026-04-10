package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsWSLFrom(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "WSL2 string",
			input:    "Linux version 5.15.146.1-microsoft-standard-WSL2",
			expected: true,
		},
		{
			name:     "WSL1 string",
			input:    "Linux version 4.4.0-microsoft",
			expected: true,
		},
		{
			name:     "case insensitive",
			input:    "Linux version 5.15.146.1-Microsoft-standard-WSL2",
			expected: true,
		},
		{
			name:     "regular Linux",
			input:    "Linux version 6.1.0-generic",
			expected: false,
		},
		{
			name:     "empty data",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWSLFrom([]byte(tt.input))
			assert.Equal(t, tt.expected, got)
		})
	}
}
