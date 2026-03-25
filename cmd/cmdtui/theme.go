package cmdtui

import "github.com/charmbracelet/lipgloss"

// Semantic color palette — adaptive for dark and light terminals.
var (
	colorSubtle    = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#5C5C5C"}
	colorHighlight = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	colorSpecial   = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}
	colorAccent    = lipgloss.AdaptiveColor{Light: "#F25D94", Dark: "#FF6B9D"}
	colorWhite     = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#FAFAFA"}
)

// Model-specific progress bar colors.
var modelColors = map[string]string{
	"opus 4.6":   "#7D56F4", // purple
	"sonnet 4.6": "#5B9CF5", // blue
	"haiku 4.5":  "#43BF6D", // green
}

// Styles used across the TUI.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorHighlight)
	subtleStyle   = lipgloss.NewStyle().Foreground(colorSubtle)
	instanceStyle = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	slugStyle     = lipgloss.NewStyle().Foreground(colorSubtle).Italic(true)
	accentStyle   = lipgloss.NewStyle().Foreground(colorAccent)
	specialStyle  = lipgloss.NewStyle().Foreground(colorSpecial)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444"))
	ruleStyle     = lipgloss.NewStyle().Foreground(colorSubtle)
)

func modelColor(name string) string {
	if c, ok := modelColors[name]; ok {
		return c
	}
	return "#7571F9" // default purple
}
