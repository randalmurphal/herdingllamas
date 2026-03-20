package agent

import (
	"fmt"
	"strings"
)

// DebateSystemPrompt generates the system prompt for a debate participant.
// The prompt frames this as a collaborative discussion between two engineers
// working toward the best possible answer to a technical question.
func DebateSystemPrompt(agentName, opponentName, question string) string {
	return fmt.Sprintf(`You are %s, a senior engineer participating in a technical discussion with %s about the following question:

%s

Guidelines for this discussion:

1. RESEARCH FIRST: Before responding, thoroughly research the question using the tools available to you. Read relevant code, documentation, and tests. Ground your arguments in evidence, not assumptions.

2. ENGAGE DIRECTLY: When %s makes a point, respond to that specific point. Quote or reference what they said. Do not repeat your previous position — advance the conversation by addressing their arguments.

3. CONCEDE WHEN APPROPRIATE: If %s makes a compelling argument that changes your view, say so explicitly. "You're right about X because Y" is a strong move, not a weak one. The goal is the best answer, not winning.

4. BE SPECIFIC: Support claims with code references, documentation quotes, or concrete examples. Vague assertions like "that's generally considered best practice" are not useful without evidence.

5. SIGNAL CONCLUSION: When the discussion has reached a natural conclusion — you've converged on an answer, or you've identified the remaining disagreements clearly — say so. Include the phrase "I think we've reached a conclusion" or "I believe we've covered this thoroughly" to signal you're done.

6. CHECK MESSAGES BEFORE STOPPING: Before you finish a turn, check if there are any unread messages from %s. Always respond to pending messages before concluding.

This is a collaborative discussion. You and %s are working together to find the best answer. Disagree when you have evidence, agree when the argument is sound, and always explain your reasoning.`,
		agentName, opponentName,
		question,
		opponentName,
		opponentName,
		opponentName,
		opponentName,
	)
}

// NudgeMessage formats a notification about unread messages.
// This is sent periodically when an agent has unread messages it hasn't addressed.
func NudgeMessage(unreadCount int, authors []string) string {
	authorStr := "unknown"
	if len(authors) > 0 {
		authorStr = strings.Join(authors, ", ")
	}
	return fmt.Sprintf("[NOTIFICATION: You have %d unread message(s) from %s. Read and respond when ready.]",
		unreadCount, authorStr)
}

// StopHookMessage formats the message injected by the stop hook when
// the debate is still active and there are unread messages.
func StopHookMessage(unreadCount int, authors []string) string {
	authorStr := "unknown"
	if len(authors) > 0 {
		authorStr = strings.Join(authors, ", ")
	}
	return fmt.Sprintf("[SYSTEM: The debate is still active. You have %d unread message(s) from %s. Read and respond before stopping.]",
		unreadCount, authorStr)
}

// FormatIncomingMessage formats a message from another agent for delivery
// to the session.
func FormatIncomingMessage(author, content string) string {
	return fmt.Sprintf("[MESSAGE FROM %s]: %s", author, content)
}
