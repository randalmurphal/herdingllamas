package tui

import "github.com/charmbracelet/lipgloss"

// Slot colors assigned to agents by registration order.
var slotColors = []lipgloss.Color{
	lipgloss.Color("#CC785C"), // Warm orange (first agent)
	lipgloss.Color("#58A6FF"), // Blue (second agent)
}

var systemColor = lipgloss.Color("#8B949E") // Gray

// Styles for message rendering.
var (
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

// AgentStyleRegistry assigns colors to agents by order of first appearance.
// The first non-system agent gets slot 0 (orange), the second gets slot 1
// (blue). System/moderator messages always use the system color.
type AgentStyleRegistry struct {
	slots    map[string]int // agent name → color slot
	nextSlot int
	// providers maps agent role names to their backing provider for display.
	providers map[string]string
}

// NewAgentStyleRegistry creates a registry pre-populated with the given
// role→provider mappings. The agentOrder slice determines color slot
// assignment (first entry gets orange, second gets blue).
func NewAgentStyleRegistry(providers map[string]string, agentOrder []string) *AgentStyleRegistry {
	r := &AgentStyleRegistry{
		slots:     make(map[string]int),
		providers: providers,
	}
	// Pre-register agents in the specified order so slot assignment is
	// deterministic regardless of which agent posts first.
	for _, name := range agentOrder {
		r.colorFor(name)
	}
	return r
}

// colorFor returns the color for an agent, assigning a new slot if needed.
func (r *AgentStyleRegistry) colorFor(agent string) lipgloss.Color {
	if agent == "system" || agent == "moderator" {
		return systemColor
	}
	slot, ok := r.slots[agent]
	if !ok {
		slot = r.nextSlot % len(slotColors)
		r.slots[agent] = slot
		r.nextSlot++
	}
	return slotColors[slot]
}

// NameStyle returns the styled name for an agent, including a provider
// annotation when available (e.g., "proponent (claude)").
func (r *AgentStyleRegistry) NameStyle(agent string) (string, lipgloss.Style) {
	color := r.colorFor(agent)
	style := lipgloss.NewStyle().Foreground(color)

	if agent == "system" || agent == "moderator" {
		return agent, style.Italic(true)
	}

	style = style.Bold(true)
	displayName := agent
	if provider, ok := r.providers[agent]; ok {
		displayName = agent + " (" + provider + ")"
	}
	return displayName, style
}

// MessageBorderStyle returns the left-border style for a given agent.
func (r *AgentStyleRegistry) MessageBorderStyle(agent string) lipgloss.Style {
	color := r.colorFor(agent)

	if agent == "system" || agent == "moderator" {
		return lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(color).
			PaddingLeft(1)
	}

	return lipgloss.NewStyle().
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(color).
		PaddingLeft(1)
}
