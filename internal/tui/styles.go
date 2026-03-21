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
	contentStyle   = lipgloss.NewStyle()

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

// Border styles for per-agent colored left borders.
var (
	claudeBorderStyle = lipgloss.NewStyle().
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(claudeColor).
				PaddingLeft(1)

	codexBorderStyle = lipgloss.NewStyle().
				BorderLeft(true).
				BorderStyle(lipgloss.ThickBorder()).
				BorderForeground(codexColor).
				PaddingLeft(1)

	systemBorderStyle = lipgloss.NewStyle().
				BorderLeft(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(systemColor).
				PaddingLeft(1)
)

// MessageBorderStyle returns the left-border style for a given agent.
func MessageBorderStyle(agent string) lipgloss.Style {
	switch agent {
	case "claude":
		return claudeBorderStyle
	case "codex":
		return codexBorderStyle
	default:
		return systemBorderStyle
	}
}

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
