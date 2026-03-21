package debate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/randalmurphal/herdingllamas/internal/agent"
	"github.com/randalmurphal/herdingllamas/internal/channel"
	"github.com/randalmurphal/herdingllamas/internal/store"
)

// EventType categorizes debate lifecycle events.
type EventType int

const (
	EventMessagePosted EventType = iota
	EventAgentStarted
	EventAgentStopped
	EventDebateStarted
	EventDebateEnded
	EventNudgeSent
	EventError
)

// Event represents a debate lifecycle event for TUI consumption.
type Event struct {
	Type      EventType
	Message   *channel.Message // Set for EventMessagePosted
	Agent     string           // Set for agent-related events
	Error     error            // Set for EventError
	Timestamp time.Time
}

// Engine orchestrates a multi-model debate. It creates agents, wires them to
// a shared channel, monitors progress, and enforces limits (turns, duration,
// budget). The TUI consumes the event stream returned by Start.
type Engine struct {
	config   Config
	store    *store.Store
	channel  *channel.Channel
	agents   []*agent.Agent
	debateID string
	logger   *slog.Logger

	events       chan Event
	done         chan struct{}
	cancel       context.CancelFunc
	hookCleanups []func()
	started      bool
	mu           sync.Mutex
}

// New creates a new debate engine. It opens the store, generates a debate ID,
// inserts the debate record, and creates the communication channel.
func New(cfg Config) (*Engine, error) {
	if cfg.Question == "" {
		return nil, fmt.Errorf("question must not be empty")
	}
	if len(cfg.Models) < 2 {
		return nil, fmt.Errorf("at least 2 models are required, got %d", len(cfg.Models))
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "."
	}

	dbPath := cfg.DBPath
	if dbPath == "" {
		defaultPath, err := store.DefaultDBPath()
		if err != nil {
			return nil, fmt.Errorf("resolving default DB path: %w", err)
		}
		dbPath = defaultPath
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}

	debateID := uuid.New().String()

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("serializing config: %w", err)
	}

	err = st.InsertDebate(store.Debate{
		ID:        debateID,
		Question:  cfg.Question,
		Config:    string(configJSON),
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("inserting debate: %w", err)
	}

	ch := channel.New(debateID, st)

	return &Engine{
		config:   cfg,
		store:    st,
		channel:  ch,
		debateID: debateID,
		logger:   slog.Default().With("debate_id", debateID),
		done:     make(chan struct{}),
	}, nil
}

// Start begins the debate. It launches agents, posts the opening question,
// and returns a channel of lifecycle events for TUI consumption. Start must
// be called exactly once.
func (e *Engine) Start(ctx context.Context) (<-chan Event, error) {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return nil, fmt.Errorf("debate engine already started")
	}
	e.started = true
	e.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	e.events = make(chan Event, 100)

	// Subscribe as "system" observer to receive all messages posted by agents.
	systemSub := e.channel.Subscribe("system")

	// Post the opening question as a moderator message so both agents see it.
	_, err := e.channel.Post("moderator", fmt.Sprintf(
		"DEBATE QUESTION: %s\n\nPlease present your arguments. You may respond to each other's points.",
		e.config.Question,
	))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("posting opening question: %w", err)
	}

	// Configure stop hooks so agents can't exit while the debate is active.
	hookScript, err := GenerateStopHookScript(e.debateID)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("generating stop hook script: %w", err)
	}

	for _, model := range e.config.Models {
		cleanup, hookErr := agent.ConfigureStopHook(agent.Provider(model), e.config.WorkDir, hookScript)
		if hookErr != nil {
			e.logger.Warn("failed to configure stop hook",
				"provider", model, "error", hookErr)
			continue
		}
		e.hookCleanups = append(e.hookCleanups, cleanup)
	}

	// Determine agent pairings. For a two-agent debate, each agent's opponent
	// is the other one. For >2 agents, opponent is set to empty (agents see
	// all messages from the channel regardless).
	models := e.config.Models
	opponentFor := func(i int) string {
		if len(models) == 2 {
			return models[1-i]
		}
		return ""
	}

	// Create each agent. agent.New creates its own LLM session internally
	// based on the provider, but does not start the run loop.
	for i, model := range models {
		nudge := e.config.NudgeInterval
		if nudge == 0 {
			nudge = 30 * time.Second
		}

		a, err := agent.New(ctx, agent.Config{
			Name:     model,
			Provider: agent.Provider(model),
			// Model is left empty so the session uses the provider's default
			// model. The "model" variable here is a provider name (e.g.
			// "claude", "codex"), not a model identifier.
			Model:         "",
			WorkDir:       e.config.WorkDir,
			Question:      e.config.Question,
			OpponentName:  opponentFor(i),
			Channel:       e.channel,
			Store:         e.store,
			DebateID:      e.debateID,
			NudgeInterval: nudge,
		})
		if err != nil {
			// Stop any agents we already started.
			for _, started := range e.agents {
				started.Stop()
			}
			cancel()
			return nil, fmt.Errorf("creating agent %s: %w", model, err)
		}
		e.agents = append(e.agents, a)
	}

	// Start each agent's run loop. Run spawns an internal goroutine and
	// returns immediately.
	for _, a := range e.agents {
		if err := a.Run(ctx); err != nil {
			for _, started := range e.agents {
				started.Stop()
			}
			cancel()
			return nil, fmt.Errorf("starting agent %s: %w", a.Name(), err)
		}

		e.emitEvent(Event{
			Type:      EventAgentStarted,
			Agent:     a.Name(),
			Timestamp: time.Now().UTC(),
		})
	}

	e.emitEvent(Event{
		Type:      EventDebateStarted,
		Timestamp: time.Now().UTC(),
	})

	// Monitor goroutine: watches messages, agent completion, and limits.
	go e.monitor(ctx, systemSub)

	return e.events, nil
}

// monitor runs in a goroutine and watches for messages, agent completion,
// and debate limits. It closes the events channel when the debate ends.
func (e *Engine) monitor(ctx context.Context, systemSub <-chan channel.Message) {
	defer close(e.events)

	// Set up deadline timer if MaxDuration is configured.
	var deadline <-chan time.Time
	if e.config.MaxDuration > 0 {
		timer := time.NewTimer(e.config.MaxDuration)
		defer timer.Stop()
		deadline = timer.C
	}

	// Track which agents are still running.
	agentDone := make(map[string]bool)

	// Merge agent Done channels into a single channel. Each goroutine sends
	// the agent name when its Done channel closes.
	agentFinished := make(chan string, len(e.agents))
	for _, a := range e.agents {
		a := a
		go func() {
			select {
			case <-a.Done():
				agentFinished <- a.Name()
			case <-ctx.Done():
				return
			}
		}()
	}

	for {
		select {
		case <-ctx.Done():
			e.emitEvent(Event{
				Type:      EventDebateEnded,
				Timestamp: time.Now().UTC(),
			})
			return

		case msg, ok := <-systemSub:
			if !ok {
				return
			}
			e.emitEvent(Event{
				Type:      EventMessagePosted,
				Message:   &msg,
				Agent:     msg.Author,
				Timestamp: msg.Timestamp,
			})

			// Update hook state so stop hooks have current unread counts.
			e.updateHookState()

			// Check turn limit. The channel's turn count includes the
			// moderator's opening message, so subtract 1 for agent turns only.
			if e.config.MaxTurns > 0 {
				agentTurns := e.channel.Len() - 1 // exclude moderator message
				if agentTurns >= e.config.MaxTurns {
					e.logger.Info("max turns reached", "turns", agentTurns)
					e.Stop()
					return
				}
			}

		case name := <-agentFinished:
			agentDone[name] = true

			var agentErr error
			for _, a := range e.agents {
				if a.Name() == name {
					agentErr = a.Err()
					break
				}
			}

			if agentErr != nil {
				e.emitEvent(Event{
					Type:      EventError,
					Agent:     name,
					Error:     agentErr,
					Timestamp: time.Now().UTC(),
				})
			}

			e.emitEvent(Event{
				Type:      EventAgentStopped,
				Agent:     name,
				Timestamp: time.Now().UTC(),
			})

			// Update hook state to reflect the stopped agent.
			e.updateHookState()

			// If all agents are done, the debate is over.
			if len(agentDone) >= len(e.agents) {
				e.logger.Info("all agents finished")
				e.Stop()
				return
			}

		case <-deadline:
			e.logger.Info("max duration reached")
			e.Stop()
			return
		}
	}
}

// Stop gracefully ends the debate. It cancels the context, stops all agents,
// updates the store, and cleans up hook state. Safe to call multiple times.
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Only close done once.
	select {
	case <-e.done:
		return nil // already stopped
	default:
	}

	if e.cancel != nil {
		e.cancel()
	}

	// Stop all agents.
	var firstErr error
	for _, a := range e.agents {
		if err := a.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Mark debate as completed in the store.
	if err := e.store.EndDebate(e.debateID, "completed"); err != nil {
		e.logger.Error("failed to end debate in store", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	// Run hook cleanup functions to remove hook configs and temp scripts.
	for _, cleanup := range e.hookCleanups {
		cleanup()
	}
	e.hookCleanups = nil

	// Update hook state to inactive so stop hooks allow exit.
	state := DebateState{
		DebateID: e.debateID,
		Active:   false,
		Agents:   make(map[string]AgentState),
	}
	for _, a := range e.agents {
		state.Agents[a.Name()] = AgentState{
			UnreadCount: 0,
			Status:      "stopped",
		}
	}
	if err := WriteState(e.debateID, state); err != nil {
		e.logger.Error("failed to write final hook state", "error", err)
	}

	if err := e.store.Close(); err != nil {
		e.logger.Error("failed to close store", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	close(e.done)
	return firstErr
}

// DebateID returns the unique identifier for this debate session.
func (e *Engine) DebateID() string {
	return e.debateID
}

// Channel returns the underlying communication channel.
func (e *Engine) Channel() *channel.Channel {
	return e.channel
}

// emitEvent sends an event to the events channel without blocking. If the
// channel buffer is full, the event is dropped and logged.
func (e *Engine) emitEvent(ev Event) {
	select {
	case e.events <- ev:
	default:
		e.logger.Warn("event channel full, dropping event",
			"type", ev.Type, "agent", ev.Agent)
	}
}

// updateHookState writes the current debate state to disk so that stop hook
// scripts can make informed decisions about whether to block agent exit.
func (e *Engine) updateHookState() {
	state := DebateState{
		DebateID: e.debateID,
		Active:   true,
		Agents:   make(map[string]AgentState),
	}

	for _, a := range e.agents {
		pending := e.channel.Pending(a.Name())
		status := "running"
		select {
		case <-a.Done():
			status = "stopped"
			if a.Err() != nil {
				status = "error"
			}
		default:
		}

		state.Agents[a.Name()] = AgentState{
			UnreadCount: pending.UnreadCount,
			Status:      status,
		}
	}

	if err := WriteState(e.debateID, state); err != nil {
		e.logger.Error("failed to write hook state", "error", err)
	}
}
