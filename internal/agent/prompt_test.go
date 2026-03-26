package agent

import (
	"strings"
	"testing"
)

func TestDebateSystemPrompt_ContainsNames(t *testing.T) {
	prompt := DebateSystemPrompt("claude", "codex", "Should we use goroutines or channels?", "/usr/local/bin/herd", "debate-123")

	if !strings.Contains(prompt, "claude") {
		t.Error("prompt should contain agent name 'claude'")
	}
	if !strings.Contains(prompt, "codex") {
		t.Error("prompt should contain opponent name 'codex'")
	}
}

func TestDebateSystemPrompt_ContainsQuestion(t *testing.T) {
	question := "Should we use goroutines or channels?"
	prompt := DebateSystemPrompt("claude", "codex", question, "/usr/local/bin/herd", "debate-123")

	if !strings.Contains(prompt, question) {
		t.Error("prompt should contain the debate question")
	}
}

func TestDebateSystemPrompt_ContainsToolCommands(t *testing.T) {
	prompt := DebateSystemPrompt("claude", "codex", "test question", "/usr/local/bin/herd", "debate-abc")

	commands := []struct {
		fragment    string
		description string
	}{
		{"/usr/local/bin/herd channel post --debate debate-abc --from claude", "should contain post command with correct binary, debate ID, and agent name"},
		{"/usr/local/bin/herd channel read --debate debate-abc --agent claude", "should contain read command with correct binary, debate ID, and agent name"},
		{"/usr/local/bin/herd channel wait --debate debate-abc --agent claude", "should contain wait command"},
		{"/usr/local/bin/herd channel conclude --debate debate-abc --from claude", "should contain conclude command with correct binary, debate ID, and agent name"},
	}

	for _, tc := range commands {
		if !strings.Contains(prompt, tc.fragment) {
			t.Errorf("prompt should contain %q: %s\nGot:\n%s", tc.fragment, tc.description, prompt)
		}
	}
}

func TestDebateSystemPrompt_ContainsKeyInstructions(t *testing.T) {
	prompt := DebateSystemPrompt("claude", "codex", "test question", "/usr/local/bin/herd", "debate-123")

	instructions := []struct {
		keyword     string
		description string
	}{
		{"RESEARCH FIRST", "should instruct agent to research before posting"},
		{"POST DELIBERATELY", "should instruct agent to post substantive contributions"},
		{"ENGAGE WITH ARGUMENTS", "should instruct agent to engage with opponent's points"},
		{"CONCEDE", "should give permission to concede"},
		{"conclude command", "should instruct agent to use the conclude command"},
		{"CHECK FOR MESSAGES", "should instruct agent to check for messages"},
		{"CONVERSATIONAL RHYTHM", "should instruct agent about conversational rhythm"},
	}

	for _, tc := range instructions {
		if !strings.Contains(prompt, tc.keyword) {
			t.Errorf("prompt should contain %q: %s", tc.keyword, tc.description)
		}
	}
}

func TestConnectorSystemPrompt_ContainsNames(t *testing.T) {
	prompt := ConnectorSystemPrompt("claude", "codex", "How should we handle distributed state?", "/usr/local/bin/herd", "debate-123")

	if !strings.Contains(prompt, "claude") {
		t.Error("prompt should contain agent name 'claude'")
	}
	if !strings.Contains(prompt, "codex") {
		t.Error("prompt should contain critic name 'codex'")
	}
}

func TestConnectorSystemPrompt_ContainsToolCommands(t *testing.T) {
	prompt := ConnectorSystemPrompt("claude", "codex", "test topic", "/usr/local/bin/herd", "debate-abc")

	commands := []struct {
		fragment    string
		description string
	}{
		{"/usr/local/bin/herd channel post --debate debate-abc --from claude", "should contain post command"},
		{"/usr/local/bin/herd channel read --debate debate-abc --agent claude", "should contain read command"},
		{"/usr/local/bin/herd channel wait --debate debate-abc --agent claude", "should contain wait command"},
		{"/usr/local/bin/herd channel conclude --debate debate-abc --from claude", "should contain conclude command"},
	}

	for _, tc := range commands {
		if !strings.Contains(prompt, tc.fragment) {
			t.Errorf("prompt should contain %q: %s", tc.fragment, tc.description)
		}
	}
}

func TestConnectorSystemPrompt_ContainsConstraints(t *testing.T) {
	prompt := ConnectorSystemPrompt("claude", "codex", "test topic", "/usr/local/bin/herd", "debate-123")

	constraints := []struct {
		keyword     string
		description string
	}{
		{"CONNECTOR", "should identify the role"},
		{"Do NOT use any keywords from the topic", "should prohibit topic-keyword searches"},
		{"STRUCTURAL PATTERN", "should instruct structural pattern identification"},
		{"IDENTIFY THE STRUCTURE FIRST", "should require structure-first thinking"},
		{"STRUCTURAL MAPPING WITH ACTIONABLE IMPLICATIONS", "should require actionable implications"},
		{"This suggests...", "should require explicit suggestion format"},
		{"PRIORITIZE SURPRISE", "should push for non-obvious insights"},
		{"DO NOT CONCLUDE EARLY", "should prevent premature conclusion"},
		{"at least 4 substantive messages", "should require minimum messages before concluding"},
	}

	for _, tc := range constraints {
		if !strings.Contains(prompt, tc.keyword) {
			t.Errorf("prompt should contain %q: %s", tc.keyword, tc.description)
		}
	}
}

func TestCriticSystemPrompt_ContainsNames(t *testing.T) {
	prompt := CriticSystemPrompt("codex", "claude", "How should we handle distributed state?", "/usr/local/bin/herd", "debate-123")

	if !strings.Contains(prompt, "codex") {
		t.Error("prompt should contain agent name 'codex'")
	}
	if !strings.Contains(prompt, "claude") {
		t.Error("prompt should contain connector name 'claude'")
	}
}

func TestCriticSystemPrompt_ContainsToolCommands(t *testing.T) {
	prompt := CriticSystemPrompt("codex", "claude", "test topic", "/usr/local/bin/herd", "debate-abc")

	commands := []struct {
		fragment    string
		description string
	}{
		{"/usr/local/bin/herd channel post --debate debate-abc --from codex", "should contain post command"},
		{"/usr/local/bin/herd channel read --debate debate-abc --agent codex", "should contain read command"},
		{"/usr/local/bin/herd channel wait --debate debate-abc --agent codex", "should contain wait command"},
		{"/usr/local/bin/herd channel conclude --debate debate-abc --from codex", "should contain conclude command"},
	}

	for _, tc := range commands {
		if !strings.Contains(prompt, tc.fragment) {
			t.Errorf("prompt should contain %q: %s", tc.fragment, tc.description)
		}
	}
}

func TestCriticSystemPrompt_ContainsConstraints(t *testing.T) {
	prompt := CriticSystemPrompt("codex", "claude", "test topic", "/usr/local/bin/herd", "debate-123")

	constraints := []struct {
		keyword     string
		description string
	}{
		{"CRITIC", "should identify the role"},
		{"RESEARCH THE TOPIC DIRECTLY", "should instruct direct research"},
		{"DO NOT PROPOSE YOUR OWN ANALOGIES", "should prohibit proposing analogies"},
		{"EXTRACT ACTIONABLE IMPLICATIONS WHEN VALIDATING", "should require actionable extraction"},
		{"DESIGN WHAT DOESN'T EXIST", "should push toward novel design"},
		{"DOESN'T EXIST YET", "should not dismiss unproven ideas"},
		{"minimum viable version", "should push for concrete design sketches"},
		{"IDENTIFY BLIND SPOTS", "should instruct identifying gaps"},
		{"PUSH FOR SPECIFICS", "should instruct pushing for specifics"},
		{"GROUND IN THE DEVELOPER'S CONTEXT", "should push for developer-grounded evaluation"},
		{"DO NOT CONCLUDE EARLY", "should prevent premature conclusion"},
		{"HOLDS / PARTIALLY HOLDS / BREAKS", "should require structured verdicts"},
	}

	for _, tc := range constraints {
		if !strings.Contains(prompt, tc.keyword) {
			t.Errorf("prompt should contain %q: %s", tc.keyword, tc.description)
		}
	}
}

func TestNudgeMessage(t *testing.T) {
	msg := NudgeMessage(3, "/usr/local/bin/herd", "debate-abc", "claude")

	wantContain := []string{
		"[NOTIFICATION:",
		"3 unread message(s)",
		"in the debate channel",
		"/usr/local/bin/herd channel read --debate debate-abc --agent claude",
	}

	for _, want := range wantContain {
		if !strings.Contains(msg, want) {
			t.Errorf("NudgeMessage() = %q, should contain %q", msg, want)
		}
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
