package claude

import "testing"

func TestInstanceStateString(t *testing.T) {
	tests := []struct {
		state InstanceState
		want  string
	}{
		{StateUnknown, "⚪"},
		{StateBusy, "🔴"},
		{StateReady, "🟢"},
		{StateBlocked, "🟡"},
		{StateWaiting, "🔵"},
		{StateError, "⚠️"},
		{StateConfirm, "🟠"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("InstanceState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestInstanceStateString_UnknownValue(t *testing.T) {
	// A value beyond the defined constants should fall through to the default case.
	s := InstanceState(99)
	if got := s.String(); got != "⚪" {
		t.Errorf("InstanceState(99).String() = %q, want ⚪", got)
	}
}
