package jira

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_hasDescription(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{"nil raw message", nil, false},
		{"empty raw message", json.RawMessage{}, false},
		{"null string", json.RawMessage(`null`), false},
		{"valid JSON object", json.RawMessage(`{"type":"doc"}`), true},
		{"empty JSON object", json.RawMessage(`{}`), true},
		{"string value", json.RawMessage(`"hello"`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasDescription(tt.raw))
		})
	}
}

func Test_nameOrEmpty(t *testing.T) {
	tests := []struct {
		name  string
		field *nameField
		want  string
	}{
		{"nil returns empty", nil, ""},
		{"display name preferred", &nameField{DisplayName: "Alice", Name: "alice"}, "Alice"},
		{"falls back to name", &nameField{Name: "bob"}, "bob"},
		{"both empty returns empty", &nameField{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nameOrEmpty(tt.field))
		})
	}
}
