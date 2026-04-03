package agent

import (
	"context"
	"sync"

	llmkit "github.com/randalmurphal/llmkit/v2"
)

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

type rootSessionAdapter struct {
	session   llmkit.Session
	steerable llmkit.SteerableSession
	outputCh  chan string
	closeOnce sync.Once
}

func NewSessionAdapter(session llmkit.Session) SessionAdapter {
	adapter := &rootSessionAdapter{
		session:  session,
		outputCh: make(chan string, 64),
	}
	if steerable, ok := session.(llmkit.SteerableSession); ok {
		adapter.steerable = steerable
	}
	go adapter.forward()
	return adapter
}

func (a *rootSessionAdapter) forward() {
	defer close(a.outputCh)

	for chunk := range a.session.Events() {
		if chunk.Type != "assistant" || chunk.Content == "" {
			continue
		}
		a.outputCh <- chunk.Content
	}
}

func (a *rootSessionAdapter) Send(ctx context.Context, content string) error {
	return a.session.Send(ctx, llmkit.Request{
		Messages: []llmkit.Message{llmkit.NewTextMessage(llmkit.RoleUser, content)},
	})
}

func (a *rootSessionAdapter) Steer(ctx context.Context, content string) error {
	if a.steerable != nil {
		return a.steerable.Steer(ctx, llmkit.Request{
			Messages: []llmkit.Message{llmkit.NewTextMessage(llmkit.RoleUser, content)},
		})
	}
	return a.Send(ctx, content)
}

func (a *rootSessionAdapter) Output() <-chan string {
	return a.outputCh
}

func (a *rootSessionAdapter) Close() error {
	var err error
	a.closeOnce.Do(func() {
		err = a.session.Close()
	})
	return err
}

func (a *rootSessionAdapter) ID() string {
	return a.session.ID()
}

func (a *rootSessionAdapter) Status() string {
	return string(a.session.Status())
}
