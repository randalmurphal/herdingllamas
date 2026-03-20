package agent

import (
	"strings"
	"testing"
)

func TestDebateSystemPrompt_ContainsNames(t *testing.T) {
	prompt := DebateSystemPrompt("claude", "codex", "Should we use goroutines or channels?")

	if !strings.Contains(prompt, "claude") {
		t.Error("prompt should contain agent name 'claude'")
	}
	if !strings.Contains(prompt, "codex") {
		t.Error("prompt should contain opponent name 'codex'")
	}
}

func TestDebateSystemPrompt_ContainsQuestion(t *testing.T) {
	question := "Should we use goroutines or channels?"
	prompt := DebateSystemPrompt("claude", "codex", question)

	if !strings.Contains(prompt, question) {
		t.Error("prompt should contain the debate question")
	}
}

func TestDebateSystemPrompt_ContainsKeyInstructions(t *testing.T) {
	prompt := DebateSystemPrompt("claude", "codex", "test question")

	instructions := []struct {
		keyword     string
		description string
	}{
		{"research", "should instruct agent to research before responding"},
		{"ENGAGE DIRECTLY", "should instruct agent to engage with specific points"},
		{"CONCEDE", "should give permission to concede"},
		{"conclusion", "should instruct agent to signal conclusion"},
		{"CHECK MESSAGES", "should instruct agent to check messages before stopping"},
		{"collaborative", "should frame as collaborative discussion"},
	}

	for _, tc := range instructions {
		if !strings.Contains(prompt, tc.keyword) {
			t.Errorf("prompt should contain %q: %s", tc.keyword, tc.description)
		}
	}
}

func TestNudgeMessage(t *testing.T) {
	tests := []struct {
		name        string
		count       int
		authors     []string
		wantContain []string
	}{
		{
			name:    "single message single author",
			count:   1,
			authors: []string{"codex"},
			wantContain: []string{
				"[NOTIFICATION:",
				"1 unread message(s)",
				"codex",
				"Read and respond when ready.",
			},
		},
		{
			name:    "multiple messages multiple authors",
			count:   3,
			authors: []string{"codex", "claude"},
			wantContain: []string{
				"3 unread message(s)",
				"codex, claude",
			},
		},
		{
			name:    "no authors falls back to unknown",
			count:   2,
			authors: nil,
			wantContain: []string{
				"2 unread message(s)",
				"unknown",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := NudgeMessage(tc.count, tc.authors)
			for _, want := range tc.wantContain {
				if !strings.Contains(msg, want) {
					t.Errorf("NudgeMessage(%d, %v) = %q, should contain %q",
						tc.count, tc.authors, msg, want)
				}
			}
		})
	}
}

func TestStopHookMessage(t *testing.T) {
	tests := []struct {
		name        string
		count       int
		authors     []string
		wantContain []string
	}{
		{
			name:    "single unread",
			count:   1,
			authors: []string{"codex"},
			wantContain: []string{
				"[SYSTEM:",
				"debate is still active",
				"1 unread message(s)",
				"codex",
				"Read and respond before stopping.",
			},
		},
		{
			name:    "no authors",
			count:   5,
			authors: nil,
			wantContain: []string{
				"5 unread message(s)",
				"unknown",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := StopHookMessage(tc.count, tc.authors)
			for _, want := range tc.wantContain {
				if !strings.Contains(msg, want) {
					t.Errorf("StopHookMessage(%d, %v) = %q, should contain %q",
						tc.count, tc.authors, msg, want)
				}
			}
		})
	}
}

func TestFormatIncomingMessage(t *testing.T) {
	msg := FormatIncomingMessage("claude", "I think we should use channels.")

	want := "[MESSAGE FROM claude]: I think we should use channels."
	if msg != want {
		t.Errorf("FormatIncomingMessage() = %q, want %q", msg, want)
	}
}

func TestFormatIncomingMessage_PreservesContent(t *testing.T) {
	content := "Here's some code:\n```go\nfmt.Println(\"hello\")\n```"
	msg := FormatIncomingMessage("codex", content)

	if !strings.Contains(msg, content) {
		t.Error("FormatIncomingMessage should preserve the full content including newlines and code blocks")
	}
	if !strings.HasPrefix(msg, "[MESSAGE FROM codex]: ") {
		t.Error("FormatIncomingMessage should start with the author prefix")
	}
}
