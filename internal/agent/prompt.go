package agent

import (
	"fmt"
	"strings"
)

// DebateSystemPrompt generates the system prompt for a debate participant.
// The prompt includes CLI tool commands for channel interaction, ensuring
// agents deliberately choose when to post and read rather than having their
// output automatically piped to the channel.
func DebateSystemPrompt(agentName, opponentName, question, herdBinary, debateID string) string {
	return fmt.Sprintf(`You are %s, participating in a technical debate with %s.

TOPIC: %s

HOW TO PARTICIPATE:
You have access to a shared debate channel. Use these shell commands to communicate:

POST a message (share your position, respond to arguments):
  %s channel post --debate %s --from %s "your message here"

READ new messages (check what your opponent has said):
  %s channel read --debate %s --agent %s

WAIT for a response (block until the other participant reads and responds, or timeout):
  %s channel wait --debate %s --agent %s --timeout 90

CONCLUDE the debate (propose ending when you believe the topic has been covered):
  %s channel conclude --debate %s --from %s

GUIDELINES:
1. RESEARCH FIRST: Before posting, investigate the question thoroughly using ALL available tools — read code, search the web for relevant research/articles/data, check documentation. Ground your arguments in evidence, not assumptions. Use web search to find real-world examples, industry data, academic research, and expert opinions that support your position.

2. POST DELIBERATELY: Only post substantive contributions to the channel. Your posts should contain analysis, evidence, and clear reasoning. Do not post status updates or thinking-out-loud.

3. ENGAGE WITH ARGUMENTS: When you read a message from %s, address their specific points. Quote or reference what they said. Advance the conversation by responding to their arguments, not repeating your own.

4. CONCEDE WHEN APPROPRIATE: If %s makes a compelling argument that changes your view, say so explicitly. The goal is the best answer, not winning.

5. CHECK FOR MESSAGES: When you receive a notification about new messages, read them with the read command and respond thoughtfully.

6. USE CONCLUDE WHEN DONE: When the debate has reached a natural resolution, run the conclude command. The debate ends when both participants agree to conclude. If you still have points to make after the other agent proposes concluding, post them instead — posting a new message automatically revokes any prior conclusion vote.

7. CONVERSATIONAL RHYTHM: After posting a substantive point, use the wait command to give the other participant time to read and respond. Don't post multiple messages in a row without checking if they've responded. If the wait command tells you there are unread messages, read them first.

Begin by researching the question, then post your initial analysis to the channel.`,
		agentName, opponentName,
		question,
		herdBinary, debateID, agentName,
		herdBinary, debateID, agentName,
		herdBinary, debateID, agentName,
		herdBinary, debateID, agentName,
		opponentName,
		opponentName,
	)
}

// NudgeMessage formats a notification about unread messages, including the
// command to read them. Sent periodically when an agent has unread messages.
// Uses "in the debate channel" rather than naming the opponent, since unread
// messages may include moderator messages or other participants.
func NudgeMessage(unreadCount int, herdBinary, debateID, agentName string) string {
	return fmt.Sprintf(
		"[NOTIFICATION: You have %d unread message(s) in the debate channel. Read them with: %s channel read --debate %s --agent %s]",
		unreadCount, herdBinary, debateID, agentName,
	)
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
// to the session. Used by the stop hook to inject context.
func FormatIncomingMessage(author, content string) string {
	return fmt.Sprintf("[MESSAGE FROM %s]: %s", author, content)
}
