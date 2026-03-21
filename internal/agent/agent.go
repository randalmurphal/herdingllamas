// Package agent wraps long-lived LLM sessions (Claude or Codex) with debate
// behavior: channel subscription, message delivery, periodic nudging, and
// structured logging to the store.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	claudesession "github.com/randalmurphal/llmkit/claude/session"
	codexsession "github.com/randalmurphal/llmkit/codex/session"

	"github.com/randalmurphal/herdingllamas/internal/channel"
	"github.com/randalmurphal/herdingllamas/internal/store"
)

// Provider identifies which LLM backend an agent uses.
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
)

// defaultNudgeInterval is the default interval between unread message checks.
const defaultNudgeInterval = 30 * time.Second

// Config configures an agent for debate participation.
type Config struct {
	Name          string
	Provider      Provider
	Model         string
	WorkDir       string
	Question      string           // The debate question (for system prompt)
	OpponentName  string           // Name of the other agent
	Channel       *channel.Channel
	Store         *store.Store
	DebateID      string
	NudgeInterval time.Duration // How often to check for unread messages (default 30s)
}

// Agent wraps a long-lived LLM session for debate participation.
type Agent struct {
	config  Config
	session SessionAdapter
	channel *channel.Channel
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
	if cfg.Channel == nil {
		return nil, fmt.Errorf("channel is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if cfg.NudgeInterval == 0 {
		cfg.NudgeInterval = defaultNudgeInterval
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
	if cfg.Channel == nil {
		return nil, fmt.Errorf("channel is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if session == nil {
		return nil, fmt.Errorf("session adapter is required")
	}
	if cfg.NudgeInterval == 0 {
		cfg.NudgeInterval = defaultNudgeInterval
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
		channel: cfg.Channel,
		store:   cfg.Store,
		logger:  logger,
		done:    make(chan struct{}),
	}
}

// createSession creates the appropriate SessionAdapter based on the provider.
func createSession(ctx context.Context, cfg Config) (SessionAdapter, error) {
	switch cfg.Provider {
	case ProviderClaude:
		return NewClaudeAdapter(ctx,
			claudesession.WithModel(cfg.Model),
			claudesession.WithWorkdir(cfg.WorkDir),
			claudesession.WithPermissions(true),
			claudesession.WithSystemPrompt(DebateSystemPrompt(cfg.Name, cfg.OpponentName, cfg.Question)),
		)
	case ProviderCodex:
		return NewCodexAdapter(ctx,
			codexsession.WithModel(cfg.Model),
			codexsession.WithWorkdir(cfg.WorkDir),
			codexsession.WithFullAuto(),
			codexsession.WithSystemPrompt(DebateSystemPrompt(cfg.Name, cfg.OpponentName, cfg.Question)),
		)
	default:
		return nil, fmt.Errorf("unknown provider: %q", cfg.Provider)
	}
}

// Run starts the agent's main loop. It subscribes to the channel, sends the
// initial question to the session, and runs three concurrent concerns:
// incoming messages, session output, and a nudge timer.
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

	// Subscribe to channel for incoming messages from the opponent.
	incoming := a.channel.Subscribe(a.config.Name)
	defer a.channel.Unsubscribe(a.config.Name)

	// Send the initial question to kick off the session. The system prompt
	// is already configured on the session via session options; this is the
	// first user message that starts the conversation.
	initialMessage := "Please begin by researching the question and sharing your initial analysis."
	if err := a.session.Send(ctx, initialMessage); err != nil {
		a.logEvent("error", map[string]string{"detail": fmt.Sprintf("sending initial message: %v", err)})
		return fmt.Errorf("sending initial message: %w", err)
	}

	nudgeTicker := time.NewTicker(a.config.NudgeInterval)
	defer nudgeTicker.Stop()

	var lastNudge time.Time

	for {
		select {
		case <-ctx.Done():
			a.logEvent("agent_stopped", map[string]string{"reason": "context_cancelled"})
			return ctx.Err()

		case msg, ok := <-incoming:
			if !ok {
				// Channel subscription was closed (e.g., channel shut down).
				a.logEvent("agent_stopped", map[string]string{"reason": "channel_closed"})
				return nil
			}
			a.handleIncomingMessage(ctx, msg)

		case text, ok := <-a.session.Output():
			if !ok {
				// Session ended — the LLM decided to stop or the session errored.
				a.logEvent("agent_stopped", map[string]string{"reason": "session_ended"})
				return nil
			}
			a.handleSessionOutput(text)

		case <-nudgeTicker.C:
			a.handleNudge(ctx, &lastNudge)
		}
	}
}

// handleIncomingMessage formats and delivers a channel message to the session.
func (a *Agent) handleIncomingMessage(ctx context.Context, msg channel.Message) {
	formatted := FormatIncomingMessage(msg.Author, msg.Content)

	// Mark the message as read so it doesn't count toward unread/nudge.
	a.channel.MarkRead(a.config.Name, msg.TurnNum+1)

	// Use Steer for Codex (mid-turn injection) or Send for Claude.
	var err error
	if a.config.Provider == ProviderCodex {
		err = a.session.Steer(ctx, formatted)
	} else {
		err = a.session.Send(ctx, formatted)
	}

	if err != nil {
		a.logger.Error("delivering message to session", "error", err, "from", msg.Author)
		return
	}

	a.logEvent("message_delivered", map[string]string{
		"from":    msg.Author,
		"turn":    fmt.Sprintf("%d", msg.TurnNum),
		"length":  fmt.Sprintf("%d", len(msg.Content)),
	})

	a.logger.Info("delivered message to session", "from", msg.Author, "turn", msg.TurnNum)
}

// handleSessionOutput posts session output to the channel.
func (a *Agent) handleSessionOutput(text string) {
	if text == "" {
		return
	}

	msg, err := a.channel.Post(a.config.Name, text)
	if err != nil {
		a.logger.Error("posting to channel", "error", err)
		return
	}

	// Advance the cursor past our own message so it doesn't count as unread
	// and trigger phantom nudges.
	a.channel.MarkRead(a.config.Name, msg.TurnNum+1)

	a.logEvent("session_output", map[string]string{
		"turn":   fmt.Sprintf("%d", msg.TurnNum),
		"length": fmt.Sprintf("%d", len(text)),
	})

	a.logger.Info("posted to channel", "length", len(text))
}

// handleNudge checks for unread messages and sends a nudge if needed.
func (a *Agent) handleNudge(ctx context.Context, lastNudge *time.Time) {
	pending := a.channel.Pending(a.config.Name)
	if pending.UnreadCount <= 0 {
		return
	}

	// Don't nudge more than once per interval (prevents rapid-fire nudges
	// if the ticker fires while a previous nudge is still being processed).
	if time.Since(*lastNudge) < a.config.NudgeInterval {
		return
	}

	nudge := NudgeMessage(pending.UnreadCount, []string{a.config.OpponentName})
	if err := a.session.Send(ctx, nudge); err != nil {
		a.logger.Error("sending nudge", "error", err)
		return
	}

	*lastNudge = time.Now()

	a.logEvent("nudge_sent", map[string]string{
		"unread_count": fmt.Sprintf("%d", pending.UnreadCount),
	})

	a.logger.Info("sent nudge", "unread", pending.UnreadCount)
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

// Stop gracefully shuts down the agent.
func (a *Agent) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	<-a.done
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
