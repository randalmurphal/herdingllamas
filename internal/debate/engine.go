package debate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/randalmurphal/herdingllamas/internal/agent"
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
	EventConclusionProposed
	EventError
)

// Event represents a debate lifecycle event for TUI consumption.
type Event struct {
	Type      EventType
	Message   *store.Message // Set for EventMessagePosted
	Agent     string         // Set for agent-related events
	Error     error          // Set for EventError
	Timestamp time.Time
}

// pollInterval is how often the monitor checks SQLite for new messages.
const pollInterval = time.Second

// Engine orchestrates a multi-model debate. It creates agents, wires them to
// the store, monitors progress via SQLite polling, and enforces limits (turns,
// duration, budget). The TUI consumes the event stream returned by Start.
type Engine struct {
	config     Config
	store      *store.Store
	agents     []*agent.Agent
	debateID   string
	herdBinary string
	logger     *slog.Logger

	events       chan Event
	done         chan struct{}
	cancel       context.CancelFunc
	hookCleanups []func()
	started      bool
	mu           sync.Mutex
}

// New creates a new debate engine. It opens the store, generates a debate ID,
// inserts the debate record, and resolves the herd binary path.
func New(cfg Config) (*Engine, error) {
	if cfg.Question == "" {
		return nil, fmt.Errorf("question must not be empty")
	}
	if len(cfg.Models) != 2 {
		return nil, fmt.Errorf("exactly 2 models are required, got %d", len(cfg.Models))
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

	// Resolve the path to the herd binary so agents can invoke channel tools.
	herdBinary, err := os.Executable()
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("resolving executable path: %w", err)
	}

	return &Engine{
		config:     cfg,
		store:      st,
		debateID:   debateID,
		herdBinary: herdBinary,
		logger:     slog.Default().With("debate_id", debateID),
		done:       make(chan struct{}),
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

	// Post the opening message as a moderator message so it appears in the
	// channel when agents run `herd channel read`.
	openingMsg := fmt.Sprintf(
		"DEBATE QUESTION: %s\n\nPlease present your arguments. You may respond to each other's points.",
		e.config.Question,
	)
	if e.config.Mode == ModeExplore {
		openingMsg = fmt.Sprintf(
			"EXPLORATION TOPIC: %s\n\nConnector: find analogies and patterns from unrelated domains. Critic: evaluate them against reality.",
			e.config.Question,
		)
	}
	if e.config.Mode == ModeInterrogate {
		openingMsg = fmt.Sprintf(
			"PLAN INTERROGATION: %s\n\nAdvocate: build the strongest evidence-based defense of this plan. Interrogator: systematically probe every dimension for gaps.",
			e.config.Question,
		)
	}
	_, err := e.store.PostMessage(e.debateID, "moderator", openingMsg)
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
	// is the other one.
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
		agentCfg := agent.Config{
			Name:     model,
			Provider: agent.Provider(model),
			// Model is left empty so the session uses the provider's default
			// model. The "model" variable here is a provider name (e.g.
			// "claude", "codex"), not a model identifier.
			Model:        "",
			WorkDir:      e.config.WorkDir,
			Question:     e.config.Question,
			OpponentName: opponentFor(i),
			Store:        e.store,
			DebateID:     e.debateID,
			HerdBinary:   e.herdBinary,
		}

		// In explore mode, the first model is the Connector (lateral
		// thinker) and the second is the Critic (reality checker).
		if e.config.Mode == ModeExplore {
			if i == 0 {
				agentCfg.SystemPrompt = agent.ConnectorSystemPrompt(
					model, opponentFor(i), e.config.Question,
					e.herdBinary, e.debateID,
				)
				agentCfg.InitialMessage = agent.ConnectorInitialMessage
			} else {
				agentCfg.SystemPrompt = agent.CriticSystemPrompt(
					model, opponentFor(i), e.config.Question,
					e.herdBinary, e.debateID,
				)
				agentCfg.InitialMessage = agent.CriticInitialMessage
			}
		}

		// In interrogate mode, the first model is the Advocate (plan
		// defender) and the second is the Interrogator (gap finder).
		if e.config.Mode == ModeInterrogate {
			if i == 0 {
				agentCfg.SystemPrompt = agent.AdvocateSystemPrompt(
					model, opponentFor(i), e.config.Question,
					e.herdBinary, e.debateID,
				)
				agentCfg.InitialMessage = agent.AdvocateInitialMessage
			} else {
				agentCfg.SystemPrompt = agent.InterrogatorSystemPrompt(
					model, opponentFor(i), e.config.Question,
					e.herdBinary, e.debateID,
				)
				agentCfg.InitialMessage = agent.InterrogatorInitialMessage
			}
		}

		a, err := agent.New(ctx, agentCfg)
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

	// Monitor goroutine: polls SQLite for new messages, watches agent
	// completion, and enforces limits.
	go e.monitor(ctx)

	return e.events, nil
}

// monitor runs in a goroutine and polls SQLite for new messages, watches for
// agent completion, and enforces debate limits.
func (e *Engine) monitor(ctx context.Context) {
	defer close(e.events)

	// Set up deadline timer if MaxDuration is configured.
	var deadline <-chan time.Time
	if e.config.MaxDuration > 0 {
		timer := time.NewTimer(e.config.MaxDuration)
		defer timer.Stop()
		deadline = timer.C
	}

	// Track message count for detecting new messages via SQLite polling.
	lastMessageCount := 0

	pollTicker := time.NewTicker(pollInterval)
	defer pollTicker.Stop()

	// Track which agents are still running.
	agentDone := make(map[string]bool)

	// Track which conclusion proposals we've already emitted events for,
	// so we don't flood the TUI on every poll tick.
	concludedSeen := make(map[string]bool)

	// Merge agent Done channels into a single channel.
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

		case <-pollTicker.C:
			messages, err := e.store.GetDebateMessages(e.debateID)
			if err != nil {
				e.logger.Error("polling messages", "error", err)
				continue
			}

			if len(messages) > lastMessageCount {
				newMessages := messages[lastMessageCount:]
				lastMessageCount = len(messages)

				// Emit events for new messages and nudge other agents.
				for _, msg := range newMessages {
					msg := msg
					e.emitEvent(Event{
						Type:      EventMessagePosted,
						Message:   &msg,
						Agent:     msg.Author,
						Timestamp: msg.Timestamp,
					})

					// Nudge all agents except the author of this message.
					// This is event-driven: agents get notified immediately
					// when there's something new to read, not on a timer.
					if msg.Author != "moderator" && msg.Author != "system" {
						for _, a := range e.agents {
							if a.Name() != msg.Author {
								unread, _ := e.store.GetUnreadCount(e.debateID, a.Name())
								if unread > 0 {
									a.Nudge(ctx, unread)
								}
							}
						}
					}
				}

				// Update hook state so stop hooks have current unread counts.
				e.updateHookState()

				// Check turn limit. Subtract 1 for the moderator's opening message.
				if e.config.MaxTurns > 0 {
					agentTurns := lastMessageCount - 1
					if agentTurns >= e.config.MaxTurns {
						e.logger.Info("max turns reached", "turns", agentTurns)
						e.emitEvent(Event{
							Type:      EventDebateEnded,
							Timestamp: time.Now().UTC(),
						})
						// Cancel context to signal agents to stop, but
						// don't call e.Stop() here. The monitor must
						// return promptly so close(e.events) runs and
						// unblocks the TUI's waitForEvent goroutine.
						// Full cleanup (agent shutdown, DB close) happens
						// when debate.go calls engine.Stop() after p.Run()
						// returns.
						e.cancel()
						return
					}
				}
			}

			// Check if all agents have concluded the debate by mutual agreement.
			concluded, err := e.store.GetConcluded(e.debateID)
			if err != nil {
				e.logger.Error("checking conclusions", "error", err)
			} else if len(concluded) >= len(e.agents) {
				e.logger.Info("all agents concluded")
				e.emitEvent(Event{
					Type:      EventDebateEnded,
					Timestamp: time.Now().UTC(),
				})
				e.cancel()
				return
			} else {
				// Emit events for newly-seen conclusion proposals.
				for _, name := range concluded {
					if !concludedSeen[name] {
						concludedSeen[name] = true
						e.emitEvent(Event{
							Type:      EventConclusionProposed,
							Agent:     name,
							Timestamp: time.Now().UTC(),
						})
					}
				}
				// If an agent revoked their conclusion (by posting a new
				// message), remove them from the seen set so we'd emit
				// again if they re-conclude.
				activeConclusions := make(map[string]bool, len(concluded))
				for _, name := range concluded {
					activeConclusions[name] = true
				}
				for name := range concludedSeen {
					if !activeConclusions[name] {
						delete(concludedSeen, name)
					}
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
				e.emitEvent(Event{
					Type:      EventDebateEnded,
					Timestamp: time.Now().UTC(),
				})
				e.cancel()
				return
			}

		case <-deadline:
			e.logger.Info("max duration reached")
			e.emitEvent(Event{
				Type:      EventDebateEnded,
				Timestamp: time.Now().UTC(),
			})
			e.cancel()
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

	// Stop all agents in parallel with a hard timeout.
	// Each agent's Stop() has its own internal timeouts, but we bound the
	// entire shutdown so the user isn't stuck waiting.
	agentErrs := make(chan error, len(e.agents))
	for _, a := range e.agents {
		a := a
		go func() {
			agentErrs <- a.Stop()
		}()
	}

	var firstErr error
	shutdownTimeout := time.After(10 * time.Second)
	for i := 0; i < len(e.agents); i++ {
		select {
		case err := <-agentErrs:
			if err != nil && firstErr == nil {
				firstErr = err
			}
		case <-shutdownTimeout:
			e.logger.Warn("agent shutdown timed out, proceeding with cleanup")
			if firstErr == nil {
				firstErr = fmt.Errorf("agent shutdown timed out")
			}
			goto cleanup
		}
	}
cleanup:

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
		unread, err := e.store.GetUnreadCount(e.debateID, a.Name())
		if err != nil {
			e.logger.Error("getting unread count for hook state", "agent", a.Name(), "error", err)
			unread = 0
		}

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
			UnreadCount: unread,
			Status:      status,
		}
	}

	if err := WriteState(e.debateID, state); err != nil {
		e.logger.Error("failed to write hook state", "error", err)
	}
}
