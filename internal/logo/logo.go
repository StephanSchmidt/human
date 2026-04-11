// Package logo renders the human CLI ASCII banner with the hero gradient.
package logo

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var art = []string{
	`██╗  ██╗██╗   ██╗███╗   ███╗ █████╗ ███╗   ██╗`,
	`██║  ██║██║   ██║████╗ ████║██╔══██╗████╗  ██║`,
	`███████║██║   ██║██╔████╔██║███████║██╔██╗ ██║`,
	`██╔══██║██║   ██║██║╚██╔╝██║██╔══██║██║╚██╗██║`,
	`██║  ██║╚██████╔╝██║ ╚═╝ ██║██║  ██║██║ ╚████║`,
	`╚═╝  ╚═╝ ╚═════╝ ╚═╝     ╚═╝╚═╝  ╚═╝╚═╝  ╚═══╝`,
}

// Hero gradient from h-l2.svg: teal -> gold -> pink.
var heroColors = []string{
	"#4ee8c4", "#52e4bc", "#58e0b4", "#60dcaa",
	"#6cd8a0", "#7cd496", "#90d08c", "#a4cc82",
	"#b8c878", "#ccc46e", "#e0c068", "#f0c468",
	"#f5c468", "#f0b866", "#eaac64", "#e4a062",
	"#de9460", "#d8885e", "#d27c5e", "#cc6c5e",
	"#c8605e", "#c45460", "#c44868", "#d04870",
	"#dc4880", "#e84898",
}

// Render returns the hero-gradient logo as a multi-line string.
// Each line is indented by two spaces.
func Render() string {
	var lines []string
	for _, line := range art {
		lines = append(lines, "  "+gradientLine(line, heroColors))
	}
	return strings.Join(lines, "\n")
}

func gradientLine(line string, colors []string) string {
	runes := []rune(line)
	width := len(runes)
	if width <= 1 {
		return line
	}
	var sb strings.Builder
	for i, r := range runes {
		ci := i * (len(colors) - 1) / (width - 1)
		s := lipgloss.NewStyle().Foreground(lipgloss.Color(colors[ci]))
		sb.WriteString(s.Render(string(r)))
	}
	return sb.String()
}
