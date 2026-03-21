package agent

import (
	"context"
	"strings"
	"sync"

	claudesession "github.com/randalmurphal/llmkit/claude/session"
)

// claudeAdapter wraps a Claude session to implement SessionAdapter.
type claudeAdapter struct {
	session   claudesession.Session
	manager   claudesession.SessionManager
	outputCh  chan string
	done      chan struct{}
	closeCh   chan struct{}
	closeOnce sync.Once
}

// NewClaudeAdapter creates a SessionAdapter backed by a Claude Code session.
// It creates a session manager, creates a session with the provided options,
// and spawns a goroutine to forward text output from the session to a string channel.
func NewClaudeAdapter(ctx context.Context, opts ...claudesession.SessionOption) (SessionAdapter, error) {
	mgr := claudesession.NewManager()

	sess, err := mgr.Create(ctx, opts...)
	if err != nil {
		_ = mgr.CloseAll()
		return nil, err
	}

	a := &claudeAdapter{
		session:  sess,
		manager:  mgr,
		outputCh: make(chan string, 64),
		done:     make(chan struct{}),
		closeCh:  make(chan struct{}),
	}

	go a.readOutput()

	return a, nil
}

// readOutput reads from the Claude session's Output channel, extracts text
// from assistant messages, and forwards it to the string output channel.
func (a *claudeAdapter) readOutput() {
	defer close(a.done)
	defer close(a.outputCh)

	// Accumulate text across assistant messages within a turn.
	// Only emit the accumulated text when a result message arrives (end of turn).
	// This filters out intermediate "I'll read the file..." messages that are
	// part of the tool-use loop and not actual debate content.
	var turnText strings.Builder

	for msg := range a.session.Output() {
		if msg.IsAssistant() {
			text := msg.GetText()
			if text != "" {
				turnText.WriteString(text)
			}
			continue
		}

		if msg.IsResult() {
			// End of turn — emit accumulated text as a single message.
			content := strings.TrimSpace(turnText.String())
			turnText.Reset()
			if content == "" {
				continue
			}
			select {
			case a.outputCh <- content:
			case <-a.closeCh:
				return
			}
			continue
		}
	}

	// Emit any remaining text if the session ended mid-turn.
	content := strings.TrimSpace(turnText.String())
	if content != "" {
		select {
		case a.outputCh <- content:
		case <-a.closeCh:
		}
	}
}

// Send sends a user message to the Claude session.
func (a *claudeAdapter) Send(ctx context.Context, content string) error {
	return a.session.Send(ctx, claudesession.NewUserMessage(content))
}

// Steer is equivalent to Send for Claude (no mid-turn injection support).
func (a *claudeAdapter) Steer(ctx context.Context, content string) error {
	return a.Send(ctx, content)
}

// Output returns a channel of text content from the session.
func (a *claudeAdapter) Output() <-chan string {
	return a.outputCh
}

// Close terminates the session and the manager.
func (a *claudeAdapter) Close() error {
	var err error
	a.closeOnce.Do(func() {
		close(a.closeCh)
		err = a.manager.CloseAll()
	})
	return err
}

// ID returns the session identifier.
func (a *claudeAdapter) ID() string {
	return a.session.ID()
}

// Status returns the session status as a string.
func (a *claudeAdapter) Status() string {
	return string(a.session.Status())
}
