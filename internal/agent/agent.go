// Package agent wraps long-lived LLM sessions (Claude or Codex) with debate
// behavior: nudge delivery and structured logging to the store.
//
// Agents communicate through the debate channel via CLI tools (herd channel
// post/read), not through session output piping. The system prompt instructs
// agents to use these tools deliberately.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	llmkit "github.com/randalmurphal/llmkit/v2"

	"github.com/randalmurphal/herdingllamas/internal/store"
)

// Provider identifies which LLM backend an agent uses.
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
)

// Config configures an agent for debate participation.
type Config struct {
	Name         string
	Provider     Provider
	Model        string // Provider-specific model ID (e.g. "opus", "gpt-5.4")
	Effort       string // Reasoning effort level (e.g. "max", "high")
	WorkDir      string
	Question     string // The debate question (for system prompt)
	OpponentName string // Name of the other agent
	Store        *store.Store
	DebateID     string
	HerdBinary   string // Path to the herd binary (for channel tool commands)

	// SystemPrompt overrides the default prompt generation. When set, used
	// directly instead of calling DebateSystemPrompt(). This lets the engine
	// pass mode-specific prompts (e.g., explore mode's Connector/Critic).
	SystemPrompt string

	// InitialMessage overrides the default first message sent to the session.
	// When empty, falls back to a generic "begin researching" message.
	InitialMessage string
}

// Agent wraps a long-lived LLM session for debate participation.
type Agent struct {
	config  Config
	session SessionAdapter
	store   *store.Store
	logger  *slog.Logger

	// Lifecycle
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

// New creates an agent with an LLM session based on the config's Provider.
// The agent is not started until Run is called.
func New(ctx context.Context, cfg Config) (*Agent, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if cfg.HerdBinary == "" {
		return nil, fmt.Errorf("herd binary path is required")
	}

	session, err := createSession(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating %s session: %w", cfg.Provider, err)
	}

	return newAgent(cfg, session), nil
}

// NewWithSession creates an agent with an externally-provided session adapter.
// Useful for testing or when the caller needs control over session creation.
// The agent is not started until Run is called.
func NewWithSession(cfg Config, session SessionAdapter) (*Agent, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if session == nil {
		return nil, fmt.Errorf("session adapter is required")
	}

	return newAgent(cfg, session), nil
}

// newAgent constructs the Agent struct. Shared by New and NewWithSession.
func newAgent(cfg Config, session SessionAdapter) *Agent {
	logger := slog.With(
		"agent", cfg.Name,
		"debate_id", cfg.DebateID,
		"provider", string(cfg.Provider),
	)

	return &Agent{
		config:  cfg,
		session: session,
		store:   cfg.Store,
		logger:  logger,
		done:    make(chan struct{}),
	}
}

// createSession creates the appropriate SessionAdapter based on the provider.
func createSession(ctx context.Context, cfg Config) (SessionAdapter, error) {
	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = DebateSystemPrompt(
			cfg.Name, cfg.OpponentName, cfg.Question,
			cfg.HerdBinary, cfg.DebateID,
		)
	}

	switch cfg.Provider {
	case ProviderClaude:
		session, err := llmkit.NewSession(ctx, string(cfg.Provider), llmkit.Config{
			Provider:        string(cfg.Provider),
			Model:           cfg.Model,
			WorkDir:         cfg.WorkDir,
			SystemPrompt:    systemPrompt,
			ReasoningEffort: cfg.Effort,
			Runtime: llmkit.RuntimeConfig{
				Providers: llmkit.RuntimeProviderConfig{
					Claude: &llmkit.ClaudeRuntimeConfig{
						DangerouslySkipPermissions: true,
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		return NewSessionAdapter(session), nil
	case ProviderCodex:
		session, err := llmkit.NewSession(ctx, string(cfg.Provider), llmkit.Config{
			Provider:        string(cfg.Provider),
			Model:           cfg.Model,
			WorkDir:         cfg.WorkDir,
			SystemPrompt:    systemPrompt,
			ReasoningEffort: cfg.Effort,
			Runtime: llmkit.RuntimeConfig{
				Providers: llmkit.RuntimeProviderConfig{
					Codex: &llmkit.CodexRuntimeConfig{
						BypassApprovalsAndSandbox: true,
					},
				},
			},
		})
		if err != nil {
			return nil, err
		}
		return NewSessionAdapter(session), nil
	default:
		return nil, fmt.Errorf("unknown provider: %q", cfg.Provider)
	}
}

// Run starts the agent's main loop. It sends the initial prompt to the session
// and runs a nudge timer that periodically checks for unread messages.
//
// Agents communicate through the debate channel via CLI tools invoked within
// their LLM sessions. The run loop does NOT read session output or pipe it
// to the channel — agents decide what to post using `herd channel post`.
//
// Run spawns an internal goroutine and returns immediately. Use Done() to
// wait for completion and Err() to check the result.
func (a *Agent) Run(ctx context.Context) error {
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return fmt.Errorf("agent already running")
	}
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.mu.Unlock()

	go func() {
		err := a.loop(ctx)
		a.mu.Lock()
		a.err = err
		a.mu.Unlock()
		close(a.done)
	}()

	return nil
}

// loop is the blocking main loop. Separated from Run so Run can set up
// lifecycle state before entering the loop.
func (a *Agent) loop(ctx context.Context) error {
	a.logEvent("agent_started", map[string]string{
		"session_id": a.session.ID(),
	})

	// Drain session output in a background goroutine so the session's
	// internal buffer doesn't block. We don't use the output for anything —
	// agents post to the channel via CLI tools.
	sessionDone := make(chan struct{})
	go func() {
		for range a.session.Output() {
		}
		close(sessionDone)
	}()

	// Send the initial prompt to kick off the session. The system prompt
	// with channel tool instructions is already configured on the session.
	initialMessage := a.config.InitialMessage
	if initialMessage == "" {
		initialMessage = "Begin by researching the question. When you have formed an initial analysis, post it to the debate channel using the channel post command in your system prompt."
	}
	if err := a.session.Send(ctx, initialMessage); err != nil {
		a.logEvent("error", map[string]string{"detail": fmt.Sprintf("sending initial message: %v", err)})
		return fmt.Errorf("sending initial message: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			a.logEvent("agent_stopped", map[string]string{"reason": "context_cancelled"})
			return ctx.Err()

		case <-sessionDone:
			// Session ended — the LLM decided to stop or the session errored.
			a.logEvent("agent_stopped", map[string]string{"reason": "session_ended"})
			return nil
		}
	}
}

// Nudge sends a notification to the agent's session that new messages are
// available in the debate channel. Called by the engine when another agent
// posts a message — not on a timer.
//
// For Codex, uses Steer (mid-turn injection) if a turn is active, since
// turn/start would fail or start a competing turn. Falls back to Send if
// Steer fails (no active turn). For Claude, Steer is equivalent to Send.
func (a *Agent) Nudge(ctx context.Context, unreadCount int) {
	if ctx.Err() != nil {
		return
	}

	nudge := NudgeMessage(unreadCount, a.config.HerdBinary, a.config.DebateID, a.config.Name)

	// Try Steer first (mid-turn injection for Codex, equivalent to Send for Claude).
	// If that fails (no active turn), fall back to Send (starts a new turn).
	err := a.session.Steer(ctx, nudge)
	if err != nil {
		err = a.session.Send(ctx, nudge)
	}
	if err != nil {
		a.logger.Error("sending nudge", "error", err)
		return
	}

	a.logEvent("nudge_sent", map[string]string{
		"unread_count": fmt.Sprintf("%d", unreadCount),
	})

	a.logger.Info("sent nudge", "unread", unreadCount)
}

// logEvent persists a structured event to the store.
func (a *Agent) logEvent(eventType string, payload map[string]string) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		a.logger.Error("marshaling event payload", "error", err)
		return
	}

	insertErr := a.store.InsertEvent(store.Event{
		DebateID:  a.config.DebateID,
		EventType: eventType,
		AgentName: a.config.Name,
		Payload:   string(payloadJSON),
		Timestamp: time.Now().UTC(),
	})
	if insertErr != nil {
		a.logger.Error("inserting event", "error", insertErr, "event_type", eventType)
	}
}

// Stop gracefully shuts down the agent. Safe to call even if Run was never
// called (returns immediately) or if called multiple times.
func (a *Agent) Stop() error {
	a.mu.Lock()
	wasStarted := a.cancel != nil
	if wasStarted {
		a.cancel()
	}
	a.mu.Unlock()

	if !wasStarted {
		return a.session.Close()
	}

	// Wait for the run loop to exit, but don't block forever.
	select {
	case <-a.done:
	case <-time.After(5 * time.Second):
		a.logger.Warn("agent loop did not exit within timeout")
	}

	return a.session.Close()
}

// Done returns a channel that's closed when the agent finishes.
func (a *Agent) Done() <-chan struct{} {
	return a.done
}

// Err returns the error that caused the agent to stop, if any.
func (a *Agent) Err() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// Name returns the agent's name.
func (a *Agent) Name() string {
	return a.config.Name
}
