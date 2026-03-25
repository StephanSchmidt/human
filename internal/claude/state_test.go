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
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("InstanceState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
