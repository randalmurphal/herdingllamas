package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/randalmurphal/herdingllamas/internal/channel"
)

// RenderMessage formats a single message as a Slack-like chat entry.
// Format:
//
//	claude  12:34:05
//	  Message content here, potentially
//	  spanning multiple lines.
func RenderMessage(msg channel.Message, width int) string {
	name := NameStyle(msg.Author).Render(msg.Author)
	ts := timestampStyle.Render(msg.Timestamp.Format("15:04:05"))
	header := fmt.Sprintf("%s  %s", name, ts)

	// Wrap content to fit within width minus 2 chars of left indent.
	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}
	wrapped := WrapText(msg.Content, contentWidth)
	body := contentStyle.Render(wrapped)

	return header + "\n" + body
}

// RenderHeader renders the debate status bar.
// Shows: debate question (truncated), agent count, message count, elapsed time.
func RenderHeader(question string, agentCount, messageCount int, elapsed time.Duration, width int) string {
	stats := fmt.Sprintf("agents: %d | msgs: %d | %s",
		agentCount, messageCount, formatDuration(elapsed))

	// Leave room for stats plus padding/spacing.
	maxQuestionWidth := width - len(stats) - 6
	truncatedQuestion := question
	if maxQuestionWidth > 3 && len(truncatedQuestion) > maxQuestionWidth {
		truncatedQuestion = truncatedQuestion[:maxQuestionWidth-3] + "..."
	}

	left := truncatedQuestion
	right := stats

	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 4 // 4 for padding
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + right
	return headerStyle.Width(width).Render(line)
}

// RenderFooter renders the controls bar.
// Shows: "q quit  |  debate-id".
func RenderFooter(debateID string, width int) string {
	left := "q quit"
	right := debateID

	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 4 // 4 for padding
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + right
	return footerStyle.Width(width).Render(line)
}

// RenderDivider renders a horizontal line.
func RenderDivider(width int) string {
	return dividerStyle.Render(strings.Repeat("─", width))
}

// RenderThinking renders a "thinking" indicator for an active agent.
func RenderThinking(agentName string) string {
	name := NameStyle(agentName).Render(agentName)
	dots := timestampStyle.Render("...")
	return fmt.Sprintf("%s %s", name, dots)
}

// WrapText wraps text to fit within a given width, preserving word boundaries.
// Lines that are already shorter than width are left as-is. Existing newlines
// are preserved.
func WrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	for i, paragraph := range strings.Split(text, "\n") {
		if i > 0 {
			result.WriteByte('\n')
		}
		result.WriteString(wrapLine(paragraph, width))
	}
	return result.String()
}

// wrapLine wraps a single line (no embedded newlines) to the given width,
// breaking at word boundaries where possible.
func wrapLine(line string, width int) string {
	if len(line) <= width {
		return line
	}

	var result strings.Builder
	words := strings.Fields(line)
	lineLen := 0

	for i, word := range words {
		wordLen := len(word)

		if i == 0 {
			// First word always goes on the first line, even if it exceeds width.
			result.WriteString(word)
			lineLen = wordLen
			continue
		}

		// Check if adding this word (plus a space) fits on the current line.
		if lineLen+1+wordLen <= width {
			result.WriteByte(' ')
			result.WriteString(word)
			lineLen += 1 + wordLen
		} else {
			// Wrap to the next line.
			result.WriteByte('\n')
			result.WriteString(word)
			lineLen = wordLen
		}
	}

	return result.String()
}

// formatDuration formats a duration as MM:SS or HH:MM:SS.
func formatDuration(d time.Duration) string {
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
