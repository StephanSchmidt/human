package githelper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTaskIDFromBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		// ClickUp native IDs (CU- prefix)
		{"CU-abc123-fix-login", "abc123"},
		{"cu-xyz789-feature", "xyz789"},
		{"CU-86a12qr7n-task-name", "86a12qr7n"},
		{"feature/CU-abc123-fix", "abc123"},
		{"fix/cu-def456-bug", "def456"},

		// Custom task IDs (PREFIX-123)
		{"PROJ-42-implement-feature", "PROJ-42"},
		{"HUM-123-add-clickup-link", "HUM-123"},
		{"feature/PROJ-42-desc", "PROJ-42"},
		{"fix/HUM-7-hotfix", "HUM-7"},
		{"AB-1", "AB-1"},

		// No match
		{"feature/something", ""},
		{"main", ""},
		{"fix-broken-thing", ""},
		{"develop", ""},

		// Excluded prefixes (false positives)
		{"FEATURE-123-something", ""},
		{"FIX-42-bug", ""},
		{"HOTFIX-7-urgent", ""},
		{"RELEASE-10-v2", ""},
		{"CI-5-pipeline", ""},

		// Edge cases
		{"CU-", ""},       // no ID after prefix
		{"CU--desc", ""},  // hyphen is not alphanumeric
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := TaskIDFromBranch(tt.branch)
			assert.Equal(t, tt.want, got)
		})
	}
}
