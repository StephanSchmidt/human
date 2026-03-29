package claude

// InstanceState represents whether a Claude Code instance is busy or ready.
type InstanceState int

const (
	StateUnknown InstanceState = iota
	StateBusy
	StateReady
	StateBlocked // waiting for permission approval
	StateWaiting // waiting for user input (AskUserQuestion, ExitPlanMode)
	StateError   // stopped due to API error or failure
)

func (s InstanceState) String() string {
	switch s {
	case StateBusy:
		return "🔴"
	case StateReady:
		return "🟢"
	case StateBlocked:
		return "🟡"
	case StateWaiting:
		return "🔵"
	case StateError:
		return "⚠️"
	default:
		return "⚪"
	}
}
