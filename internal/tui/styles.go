package tui

import "github.com/charmbracelet/lipgloss"

// Agent-specific colors.
var (
	claudeColor = lipgloss.Color("#CC785C") // Warm orange
	codexColor  = lipgloss.Color("#58A6FF") // Blue
	systemColor = lipgloss.Color("#8B949E") // Gray
)

// Styles for message rendering.
var (
	claudeNameStyle = lipgloss.NewStyle().Foreground(claudeColor).Bold(true)
	codexNameStyle  = lipgloss.NewStyle().Foreground(codexColor).Bold(true)
	systemNameStyle = lipgloss.NewStyle().Foreground(systemColor).Italic(true)

	timestampStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6E7681")).Faint(true)
	contentStyle   = lipgloss.NewStyle().PaddingLeft(2)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#E6EDF3")).
			Background(lipgloss.Color("#161B22")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6E7681")).
			Padding(0, 1)

	dividerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#30363D"))
)

// NameStyle returns the appropriate style for an agent name.
func NameStyle(agent string) lipgloss.Style {
	switch agent {
	case "claude":
		return claudeNameStyle
	case "codex":
		return codexNameStyle
	default:
		return systemNameStyle
	}
}
