package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/randalmurphal/herdingllamas/internal/debate"
	"github.com/randalmurphal/herdingllamas/internal/store"
)

// Model is the bubbletea model for the debate TUI.
type Model struct {
	// Debate state
	events    <-chan debate.Event
	engine    *debate.Engine
	question  string
	debateID  string
	messages  []store.Message
	agents    map[string]bool // agent name -> still active
	startTime time.Time

	// TUI state
	viewport    viewport.Model
	width       int
	height      int
	ready       bool
	quitting    bool
	debateEnded bool

	// Rendering
	content string // Full rendered content for viewport
	styles  *AgentStyleRegistry
}

// New creates a new TUI model. The providers map pairs agent role names
// (e.g., "proponent") with their backing provider (e.g., "claude") for
// display annotations in the chat view. agentOrder determines which agent
// gets which color slot (first = orange, second = blue).
func New(engine *debate.Engine, events <-chan debate.Event, question string, providers map[string]string, agentOrder []string) Model {
	return Model{
		events:    events,
		engine:    engine,
		question:  question,
		debateID:  engine.DebateID(),
		agents:    make(map[string]bool),
		startTime: time.Now(),
		styles:    NewAgentStyleRegistry(providers, agentOrder),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		waitForEvent(m.events),
		tickCmd(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return handleUpdate(m, msg)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "Initializing debate..."
	}

	header := RenderHeader(
		m.question,
		len(m.agents),
		len(m.messages),
		time.Since(m.startTime),
		m.width,
		m.debateEnded,
	)
	topDivider := RenderDivider(m.width)
	bottomDivider := RenderDivider(m.width)
	footer := RenderFooter(m.debateID, m.width, m.debateEnded)

	return header + "\n" +
		topDivider + "\n" +
		m.viewport.View() + "\n" +
		bottomDivider + "\n" +
		footer
}
