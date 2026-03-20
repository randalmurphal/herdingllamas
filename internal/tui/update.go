package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/randalmurphal/herdingllamas/internal/channel"
	"github.com/randalmurphal/herdingllamas/internal/debate"
)

// debateEventMsg wraps a debate.Event for the bubbletea message loop.
type debateEventMsg debate.Event

// tickMsg is sent every second to update the elapsed time display.
type tickMsg struct{}

// waitForEvent returns a tea.Cmd that reads the next event from the debate
// engine's event channel. When the channel closes, it sends a debateEventMsg
// with EventDebateEnded type.
func waitForEvent(events <-chan debate.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return debateEventMsg(debate.Event{
				Type:      debate.EventDebateEnded,
				Timestamp: time.Now().UTC(),
			})
		}
		return debateEventMsg(ev)
	}
}

// tickCmd returns a tea.Cmd that sends a tickMsg after one second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

// chromeHeight is the total number of terminal lines consumed by non-viewport
// elements in View(): header(1) + "\n"(1) + topDivider(1) + "\n"(1) +
// [viewport] + "\n"(1) + bottomDivider(1) + "\n"(1) + footer(1) = 8.
const chromeHeight = 8

// handleUpdate processes a single bubbletea message and returns the updated
// model and any follow-up commands.
func handleUpdate(m Model, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		viewportHeight := m.height - chromeHeight
		if viewportHeight < 1 {
			viewportHeight = 1
		}

		if !m.ready {
			m.viewport = viewport.New(m.width, viewportHeight)
			m.viewport.SetContent(m.content)
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = viewportHeight
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		// Pass other keys to the viewport for scrolling.
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case debateEventMsg:
		ev := debate.Event(msg)
		return handleDebateEvent(m, ev)

	case tickMsg:
		// Re-render the header with updated elapsed time by returning a tick
		// command. The View() function always reads time.Since(m.startTime),
		// so simply scheduling the next tick is enough.
		return m, tickCmd()
	}

	// Pass unhandled messages to the viewport.
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// handleDebateEvent processes a debate engine event, updates model state,
// and re-renders the viewport content.
func handleDebateEvent(m Model, ev debate.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case debate.EventMessagePosted:
		if ev.Message != nil {
			m.messages = append(m.messages, *ev.Message)
			m.content = renderAllMessages(m.messages, m.width)
			if m.ready {
				m.viewport.SetContent(m.content)
				m.viewport.GotoBottom()
			}
		}

	case debate.EventAgentStarted:
		m.agents[ev.Agent] = true

	case debate.EventAgentStopped:
		m.agents[ev.Agent] = false

	case debate.EventDebateEnded:
		// Mark all agents as inactive but don't quit -- let the user
		// scroll through the debate and quit manually with 'q'.
		for name := range m.agents {
			m.agents[name] = false
		}
		// No follow-up event read since the channel is closed.
		return m, nil

	case debate.EventError:
		// Render errors as system messages so they appear in the chat.
		if ev.Error != nil {
			errMsg := "Error"
			if ev.Agent != "" {
				errMsg += " (" + ev.Agent + ")"
			}
			errMsg += ": " + ev.Error.Error()

			// Synthesize a system message for display.
			sysMsg := synthesizeSystemMessage(errMsg)
			m.messages = append(m.messages, sysMsg)
			m.content = renderAllMessages(m.messages, m.width)
			if m.ready {
				m.viewport.SetContent(m.content)
				m.viewport.GotoBottom()
			}
		}
	}

	return m, waitForEvent(m.events)
}

// renderAllMessages rebuilds the full viewport content from all messages.
func renderAllMessages(messages []channel.Message, width int) string {
	if len(messages) == 0 {
		return ""
	}

	var b strings.Builder
	for i, msg := range messages {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(RenderMessage(msg, width))
	}
	return b.String()
}

// synthesizeSystemMessage creates a channel.Message for display purposes
// (e.g., errors, status updates) that did not originate from an agent.
func synthesizeSystemMessage(text string) channel.Message {
	return channel.Message{
		Author:    "system",
		Content:   text,
		Timestamp: time.Now().UTC(),
	}
}
