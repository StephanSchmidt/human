package cmdtui

import "github.com/charmbracelet/lipgloss"

// Human brand colors.
const (
	humanGold   = lipgloss.Color("#fac86a") // warm gold — titles, highlights
	humanYellow = lipgloss.Color("#ffee97") // light yellow — instance labels
	humanPink   = lipgloss.Color("#d73d73") // pink — errors
	humanTeal   = lipgloss.Color("#4ee8c4") // teal — success, ready, running
	humanPurple = lipgloss.Color("#555598") // muted purple — subtle, secondary
	humanRed    = lipgloss.Color("#e05050") // red — busy, working, accent
)

// Model-specific progress bar colors.
var modelColors = map[string]string{
	"opus 4.6":   "#fac86a", // gold
	"sonnet 4.6": "#4ee8c4", // teal
	"haiku 4.5":  "#555598", // purple
}

// Styles used across the TUI.
var (
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(humanGold)
	subtleStyle       = lipgloss.NewStyle().Foreground(humanPurple)
	busyInstanceStyle = lipgloss.NewStyle().Bold(true).Foreground(humanPink)
	idleInstanceStyle = lipgloss.NewStyle().Bold(true).Foreground(humanTeal)
	slugStyle         = lipgloss.NewStyle().Foreground(humanPurple).Italic(true)
	accentStyle       = lipgloss.NewStyle().Foreground(humanRed)
	specialStyle      = lipgloss.NewStyle().Foreground(humanTeal)
	warningStyle      = lipgloss.NewStyle().Foreground(humanYellow)
	errorStyle        = lipgloss.NewStyle().Foreground(humanPink)
	ruleStyle         = lipgloss.NewStyle().Foreground(humanPurple)
)

func modelColor(name string) string {
	if c, ok := modelColors[name]; ok {
		return c
	}
	return "#fac86a" // default gold
}
