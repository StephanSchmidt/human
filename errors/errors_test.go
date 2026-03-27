package errors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_isFormatVerb(t *testing.T) {
	tests := []struct {
		name string
		c    byte
		want bool
	}{
		{"d is a verb", 'd', true},
		{"s is a verb", 's', true},
		{"v is a verb", 'v', true},
		{"w is a verb", 'w', true},
		{"f is a verb", 'f', true},
		{"q is a verb", 'q', true},
		{"x is a verb", 'x', true},
		{"t is a verb", 't', true},
		{"percent is not a verb", '%', false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isFormatVerb(tt.c))
		})
	}
}

func Test_extractArgs(t *testing.T) {
	tests := []struct {
		name    string
		message string
		details []any
		want    []any
	}{
		{
			name:    "no placeholders no details",
			message: "simple message",
			details: nil,
			want:    nil,
		},
		{
			name:    "one placeholder one pair",
			message: "user %s failed",
			details: []any{"name", "alice"},
			want:    []any{"alice"},
		},
		{
			name:    "more args than placeholders truncates",
			message: "user %s failed",
			details: []any{"name", "alice", "code", 42},
			want:    []any{"alice"},
		},
		{
			name:    "fewer args than placeholders keeps all",
			message: "user %s code %d",
			details: []any{"name", "alice"},
			want:    []any{"alice"},
		},
		{
			name:    "multiple verbs",
			message: "%s returned %d with %v",
			details: []any{"op", "get", "status", 404, "body", "not found"},
			want:    []any{"get", 404, "not found"},
		},
		{
			name:    "percent-w is counted",
			message: "wrapping %w with %s",
			details: []any{"key", "val"},
			want:    []any{"val"},
		},
		{
			name:    "empty details",
			message: "msg %s",
			details: []any{},
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArgs(tt.message, tt.details)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWithDetails(t *testing.T) {
	err := WithDetails("operation %s failed with code %d",
		"op", "create", "code", 500)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "operation create failed with code 500")

	details := AllDetails(err)
	assert.Equal(t, "create", details["op"])
	assert.Equal(t, 500, details["code"])
}

func TestWrapWithDetails(t *testing.T) {
	cause := WithDetails("root cause")
	err := WrapWithDetails(cause, "wrapping %s",
		"key", "val")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrapping val")

	details := AllDetails(err)
	assert.Equal(t, "val", details["key"])
}
