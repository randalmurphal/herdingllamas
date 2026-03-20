package agent

import (
	"context"
	"sync"

	codexsession "github.com/randalmurphal/llmkit/codex/session"
)

// codexAdapter wraps a Codex session to implement SessionAdapter.
type codexAdapter struct {
	session   codexsession.Session
	manager   codexsession.SessionManager
	outputCh  chan string
	done      chan struct{}
	closeCh   chan struct{}
	closeOnce sync.Once
}

// NewCodexAdapter creates a SessionAdapter backed by a Codex app-server session.
// It creates a session manager, creates a session with the provided options,
// and spawns a goroutine to forward text output from the session to a string channel.
func NewCodexAdapter(ctx context.Context, opts ...codexsession.SessionOption) (SessionAdapter, error) {
	mgr := codexsession.NewManager()

	sess, err := mgr.Create(ctx, opts...)
	if err != nil {
		_ = mgr.CloseAll()
		return nil, err
	}

	a := &codexAdapter{
		session:  sess,
		manager:  mgr,
		outputCh: make(chan string, 64),
		done:     make(chan struct{}),
		closeCh:  make(chan struct{}),
	}

	go a.readOutput()

	return a, nil
}

// readOutput reads from the Codex session's Output channel, extracts text
// from agent_message items, and forwards it to the string output channel.
func (a *codexAdapter) readOutput() {
	defer close(a.done)
	defer close(a.outputCh)

	for msg := range a.session.Output() {
		if !msg.IsAgentMessage() {
			continue
		}
		text := msg.GetText()
		if text == "" {
			continue
		}
		select {
		case a.outputCh <- text:
		case <-a.closeCh:
			return
		}
	}
}

// Send sends a user message to the Codex session via turn/start.
func (a *codexAdapter) Send(ctx context.Context, content string) error {
	return a.session.Send(ctx, codexsession.NewUserMessage(content))
}

// Steer injects input into an actively running turn via turn/steer.
func (a *codexAdapter) Steer(ctx context.Context, content string) error {
	return a.session.Steer(ctx, codexsession.NewUserMessage(content))
}

// Output returns a channel of text content from the session.
func (a *codexAdapter) Output() <-chan string {
	return a.outputCh
}

// Close terminates the session and the manager.
func (a *codexAdapter) Close() error {
	var err error
	a.closeOnce.Do(func() {
		close(a.closeCh)
		err = a.manager.CloseAll()
	})
	return err
}

// ID returns the session identifier.
func (a *codexAdapter) ID() string {
	return a.session.ID()
}

// Status returns the session status as a string.
func (a *codexAdapter) Status() string {
	return string(a.session.Status())
}
