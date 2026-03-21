package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/randalmurphal/herdingllamas/internal/store"
)

func TestRenderMessage_FormatAndContent(t *testing.T) {
	ts := time.Date(2025, 1, 15, 12, 34, 5, 0, time.UTC)
	msg := store.Message{
		Author:    "claude",
		Content:   "I think Postgres is the better choice.",
		Timestamp: ts,
	}

	result := RenderMessage(msg, 80)

	// The rendered output should contain the agent name, timestamp, and content.
	if !strings.Contains(result, "claude") {
		t.Errorf("expected agent name 'claude' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "12:34:05") {
		t.Errorf("expected timestamp '12:34:05' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "I think Postgres is the better choice.") {
		t.Errorf("expected message content in output, got:\n%s", result)
	}
}

func TestRenderMessage_MultilineContent(t *testing.T) {
	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	msg := store.Message{
		Author:    "codex",
		Content:   "Line one.\nLine two.",
		Timestamp: ts,
	}

	result := RenderMessage(msg, 80)

	if !strings.Contains(result, "Line one.") {
		t.Errorf("expected first line in output, got:\n%s", result)
	}
	if !strings.Contains(result, "Line two.") {
		t.Errorf("expected second line in output, got:\n%s", result)
	}
}

func TestRenderMessage_SystemMessage(t *testing.T) {
	ts := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	msg := store.Message{
		Author:    "system",
		Content:   "Debate started.",
		Timestamp: ts,
	}

	result := RenderMessage(msg, 80)

	if !strings.Contains(result, "system") {
		t.Errorf("expected 'system' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "Debate started.") {
		t.Errorf("expected content in output, got:\n%s", result)
	}
}

func TestRenderHeader_ShowsQuestionAndStats(t *testing.T) {
	result := RenderHeader("Should we use Postgres?", 2, 5, 3*time.Minute+42*time.Second, 80, false)

	if !strings.Contains(result, "Should we use Postgres?") {
		t.Errorf("expected question in header, got:\n%s", result)
	}
	if !strings.Contains(result, "LIVE") {
		t.Errorf("expected LIVE status in header, got:\n%s", result)
	}
	if !strings.Contains(result, "agents: 2") {
		t.Errorf("expected agent count in header, got:\n%s", result)
	}
	if !strings.Contains(result, "msgs: 5") {
		t.Errorf("expected message count in header, got:\n%s", result)
	}
	if !strings.Contains(result, "3:42") {
		t.Errorf("expected elapsed time in header, got:\n%s", result)
	}
}

func TestRenderHeader_TruncatesLongQuestion(t *testing.T) {
	longQuestion := strings.Repeat("x", 200)
	result := RenderHeader(longQuestion, 1, 1, time.Second, 80, false)

	if strings.Contains(result, longQuestion) {
		t.Errorf("expected long question to be truncated, got:\n%s", result)
	}
	if !strings.Contains(result, "...") {
		t.Errorf("expected truncation ellipsis in header, got:\n%s", result)
	}
}

func TestRenderFooter_ShowsControlsAndDebateID(t *testing.T) {
	result := RenderFooter("abc-123", 80, false)

	if !strings.Contains(result, "q quit") {
		t.Errorf("expected 'q quit' in footer, got:\n%s", result)
	}
	if !strings.Contains(result, "abc-123") {
		t.Errorf("expected debate ID in footer, got:\n%s", result)
	}
}

func TestRenderHeader_ShowsEndedStatus(t *testing.T) {
	result := RenderHeader("Test question", 2, 10, time.Minute, 80, true)

	if !strings.Contains(result, "ENDED") {
		t.Errorf("expected ENDED status in header, got:\n%s", result)
	}
	if strings.Contains(result, "LIVE") {
		t.Errorf("expected no LIVE status when ended, got:\n%s", result)
	}
}

func TestRenderFooter_ShowsDebateEndedHint(t *testing.T) {
	result := RenderFooter("abc-123", 80, true)

	if !strings.Contains(result, "debate ended") {
		t.Errorf("expected 'debate ended' in footer, got:\n%s", result)
	}
	if !strings.Contains(result, "q quit") {
		t.Errorf("expected 'q quit' in footer, got:\n%s", result)
	}
}

func TestRenderDivider_FillsWidth(t *testing.T) {
	result := RenderDivider(40)

	// The divider should contain the line character repeated.
	if !strings.Contains(result, "─") {
		t.Errorf("expected divider character in output, got:\n%s", result)
	}
}

func TestRenderThinking_ShowsAgentName(t *testing.T) {
	result := RenderThinking("claude")

	if !strings.Contains(result, "claude") {
		t.Errorf("expected agent name in thinking indicator, got:\n%s", result)
	}
	if !strings.Contains(result, "...") {
		t.Errorf("expected dots in thinking indicator, got:\n%s", result)
	}
}

func TestWrapText_ShortText(t *testing.T) {
	result := WrapText("hello world", 80)
	if result != "hello world" {
		t.Errorf("expected unchanged short text, got: %q", result)
	}
}

func TestWrapText_WrapsAtWordBoundary(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog"
	result := WrapText(text, 20)

	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if len(line) > 20 {
			t.Errorf("line %d exceeds width 20: %q (len=%d)", i, line, len(line))
		}
	}

	// All words should still be present.
	for _, word := range strings.Fields(text) {
		if !strings.Contains(result, word) {
			t.Errorf("missing word %q in wrapped output:\n%s", word, result)
		}
	}
}

func TestWrapText_PreservesNewlines(t *testing.T) {
	text := "line one\nline two\nline three"
	result := WrapText(text, 80)

	if result != text {
		t.Errorf("expected newlines preserved, got: %q", result)
	}
}

func TestWrapText_ZeroWidth(t *testing.T) {
	text := "hello"
	result := WrapText(text, 0)
	if result != text {
		t.Errorf("expected unchanged text with zero width, got: %q", result)
	}
}

func TestWrapText_LongWord(t *testing.T) {
	// A single word longer than the width should not be broken.
	text := "supercalifragilisticexpialidocious"
	result := WrapText(text, 10)
	if result != text {
		t.Errorf("expected long word left intact, got: %q", result)
	}
}

func TestNameStyle_Claude(t *testing.T) {
	style := NameStyle("claude")
	// Verify it returns the claude-specific style by rendering and checking
	// it produces non-empty output containing the name.
	rendered := style.Render("claude")
	if !strings.Contains(rendered, "claude") {
		t.Errorf("expected 'claude' in rendered name, got: %q", rendered)
	}
}

func TestNameStyle_Codex(t *testing.T) {
	style := NameStyle("codex")
	rendered := style.Render("codex")
	if !strings.Contains(rendered, "codex") {
		t.Errorf("expected 'codex' in rendered name, got: %q", rendered)
	}
}

func TestNameStyle_Unknown(t *testing.T) {
	style := NameStyle("other-agent")
	rendered := style.Render("other-agent")
	if !strings.Contains(rendered, "other-agent") {
		t.Errorf("expected 'other-agent' in rendered name, got: %q", rendered)
	}
}

func TestFormatDuration_MinutesAndSeconds(t *testing.T) {
	result := formatDuration(3*time.Minute + 42*time.Second)
	if result != "3:42" {
		t.Errorf("expected '3:42', got: %q", result)
	}
}

func TestFormatDuration_WithHours(t *testing.T) {
	result := formatDuration(1*time.Hour + 5*time.Minute + 9*time.Second)
	if result != "1:05:09" {
		t.Errorf("expected '1:05:09', got: %q", result)
	}
}

func TestFormatDuration_Zero(t *testing.T) {
	result := formatDuration(0)
	if result != "0:00" {
		t.Errorf("expected '0:00', got: %q", result)
	}
}
