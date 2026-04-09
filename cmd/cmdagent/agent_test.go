package cmdagent

import (
	"strings"
	"testing"
)

func TestBuildAgentCmd_hasSubcommands(t *testing.T) {
	cmd := BuildAgentCmd()

	want := []string{"start", "stop", "list", "attach"}
	subs := cmd.Commands()

	found := make(map[string]bool)
	for _, sub := range subs {
		found[sub.Name()] = true
	}

	for _, w := range want {
		if !found[w] {
			t.Errorf("missing subcommand %q", w)
		}
	}
}

func TestBuildAgentCmd_startRequiresName(t *testing.T) {
	cmd := BuildAgentCmd()
	cmd.SetArgs([]string{"start"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when name is missing, got nil")
	}
	if !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildAgentCmd_stopRequiresName(t *testing.T) {
	cmd := BuildAgentCmd()
	cmd.SetArgs([]string{"stop"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when name is missing, got nil")
	}
}

func TestBuildAgentCmd_listNoArgs(t *testing.T) {
	cmd := BuildAgentCmd()
	cmd.SetArgs([]string{"list", "extra"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when list gets extra args, got nil")
	}
}
