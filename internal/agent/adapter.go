package agent

import "context"

// SessionAdapter abstracts over Claude and Codex long-lived sessions.
// Both support Send and Output; Codex additionally supports Steer (mid-turn injection).
type SessionAdapter interface {
	// Send sends a user message to the session. Returns immediately;
	// responses arrive via the Output channel.
	Send(ctx context.Context, content string) error

	// Steer injects input into an actively running turn.
	// For Claude, this is equivalent to Send (no mid-turn injection).
	// For Codex, this calls turn/steer on the app-server.
	Steer(ctx context.Context, content string) error

	// Output returns a channel of text content from the session.
	// The channel is closed when the session ends.
	Output() <-chan string

	// Close terminates the session.
	Close() error

	// ID returns the session identifier.
	ID() string

	// Status returns the session status as a string.
	Status() string
}
