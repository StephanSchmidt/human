package claude

// InstanceState represents whether a Claude Code instance is busy or ready.
type InstanceState int

const (
	StateUnknown InstanceState = iota
	StateBusy
	StateReady
)

func (s InstanceState) String() string {
	switch s {
	case StateBusy:
		return "🔴"
	case StateReady:
		return "🟢"
	default:
		return "⚪"
	}
}
