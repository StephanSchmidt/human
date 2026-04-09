package agent

import (
	"testing"
)

func TestIsValidName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"agent1", true},
		{"my-agent", true},
		{"my_agent", true},
		{"Agent-1", true},
		{"-invalid", false},
		{"_invalid", false},
		{"", false},
		{"has space", false},
		{"has.dot", false},
	}

	for _, tt := range tests {
		if got := isValidName(tt.name); got != tt.valid {
			t.Errorf("isValidName(%q) = %v, want %v", tt.name, got, tt.valid)
		}
	}
}

func TestBuildClaudeArgs(t *testing.T) {
	mgr := &Manager{}

	args := mgr.BuildClaudeArgs(StartOpts{})
	if len(args) != 1 || args[0] != "--permission-mode=auto" {
		t.Errorf("default args = %v", args)
	}

	args = mgr.BuildClaudeArgs(StartOpts{SkipPerms: true, Model: "opus"})
	found := map[string]bool{}
	for _, a := range args {
		found[a] = true
	}
	if !found["--dangerously-skip-permissions"] {
		t.Error("missing --dangerously-skip-permissions")
	}
	if !found["opus"] {
		t.Error("missing model opus")
	}
}
