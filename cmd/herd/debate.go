package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/randalmurphal/herdingllamas/internal/debate"
	"github.com/randalmurphal/herdingllamas/internal/store"
	"github.com/randalmurphal/herdingllamas/internal/tui"
)

// debateJSON is the JSON output format for --json mode.
type debateJSON struct {
	DebateID  string          `json:"debate_id"`
	Question  string          `json:"question"`
	Agents    []agentMetaJSON `json:"agents"`
	Status    string          `json:"status"`
	StartedAt time.Time      `json:"started_at"`
	EndedAt   *time.Time      `json:"ended_at,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Messages  []messageJSON   `json:"messages"`
}

type agentMetaJSON struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
}

type messageJSON struct {
	Turn      int       `json:"turn"`
	Author    string    `json:"author"`
	Provider  string    `json:"provider,omitempty"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

func debateCmd() *cobra.Command {
	var flags commonFlags

	cmd := &cobra.Command{
		Use:   "debate [question]",
		Short: "Start a multi-model debate",
		Long:  "Starts an interactive debate between LLM agents on the given question.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			question := args[0]

			rc, err := resolveFromFlags(ctx, cmd, flags, debate.ModeDebate)
			if err != nil {
				return err
			}

			cfg := debate.Config{
				Question:        question,
				Models:          rc.Models,
				WorkDir:         flags.workDir,
				MaxTurns:        flags.maxTurns,
				MaxDuration:     flags.maxDuration,
				ModelOverrides:  rc.ModelOverrides,
				EffortOverrides: rc.EffortOverrides,
			}

			if !flags.jsonOutput {
				fmt.Println("Creating debate engine...")
			}
			engine, err := debate.New(cfg, rc.AgentMeta)
			if err != nil {
				return fmt.Errorf("create debate engine: %w", err)
			}

			if !flags.jsonOutput {
				printAgentTable(rc.AgentMeta)
				fmt.Println("(This may take a moment while sessions initialize. Ctrl+C to abort.)")
			}
			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start debate: %w", err)
			}

			if flags.jsonOutput {
				for range events {
				}
				engine.Stop()
				return outputDebateJSON(ctx, engine.DebateID(), rc.AgentMeta, !flags.noSummary)
			}

			fmt.Println("Agents ready. Launching debate TUI...")
			providers := agentMetaToProviders(rc.AgentMeta)
			m := tui.New(engine, events, question, providers, agentMetaOrder(rc.AgentMeta))
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			engine.Stop()
			fmt.Printf("\nDebate %s saved to database.\n", engine.DebateID())

			if !flags.noSummary {
				printDebateSummary(ctx, engine.DebateID())
			}

			return nil
		},
	}

	registerCommonFlags(cmd, &flags)
	return cmd
}

// outputDebateJSON writes the completed debate as JSON to stdout.
func outputDebateJSON(ctx context.Context, debateID string, agentMeta []debate.AgentMeta, includeSummary bool) error {
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		return fmt.Errorf("resolving DB path: %w", err)
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer st.Close()

	d, err := st.GetDebate(debateID)
	if err != nil {
		return fmt.Errorf("getting debate: %w", err)
	}
	if d == nil {
		return fmt.Errorf("debate not found: %s", debateID)
	}

	messages, err := st.GetDebateMessages(debateID)
	if err != nil {
		return fmt.Errorf("getting messages: %w", err)
	}

	// Build role→provider lookup for message annotation.
	providerFor := make(map[string]string, len(agentMeta))
	agents := make([]agentMetaJSON, 0, len(agentMeta))
	for _, am := range agentMeta {
		providerFor[am.Role] = am.Provider
		agents = append(agents, agentMetaJSON{
			Role:     am.Role,
			Provider: am.Provider,
		})
	}

	out := debateJSON{
		DebateID:  d.ID,
		Question:  d.Question,
		Agents:    agents,
		Status:    d.Status,
		StartedAt: d.CreatedAt,
		EndedAt:   d.EndedAt,
		Messages:  make([]messageJSON, 0, len(messages)),
	}

	for _, msg := range messages {
		out.Messages = append(out.Messages, messageJSON{
			Turn:      msg.TurnNum,
			Author:    msg.Author,
			Provider:  providerFor[msg.Author],
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}

	if includeSummary {
		transcript := formatTranscript(d, messages)
		mode := extractMode(d)
		summary, err := generateSummary(ctx, d, transcript, mode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate summary: %v\n", err)
		} else {
			out.Summary = summary
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// agentMetaToProviders converts AgentMeta to a role→provider map for TUI use.
func agentMetaToProviders(meta []debate.AgentMeta) map[string]string {
	m := make(map[string]string, len(meta))
	for _, am := range meta {
		m[am.Role] = am.Provider
	}
	return m
}

// agentMetaOrder extracts the ordered role names from AgentMeta for
// deterministic color slot assignment.
func agentMetaOrder(meta []debate.AgentMeta) []string {
	order := make([]string, len(meta))
	for i, am := range meta {
		order[i] = am.Role
	}
	return order
}

// printDebateSummary generates and prints a summary after the TUI exits.
func printDebateSummary(ctx context.Context, debateID string) {
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve DB path for summary: %v\n", err)
		return
	}

	st, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open store for summary: %v\n", err)
		return
	}
	defer st.Close()

	d, err := st.GetDebate(debateID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get debate for summary: %v\n", err)
		return
	}
	if d == nil {
		fmt.Fprintf(os.Stderr, "Warning: debate %s not found for summary\n", debateID)
		return
	}

	messages, err := st.GetDebateMessages(debateID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get messages for summary: %v\n", err)
		return
	}
	if len(messages) == 0 {
		return
	}

	fmt.Fprintf(os.Stderr, "\nGenerating summary...\n")
	transcript := formatTranscript(d, messages)
	mode := extractMode(d)
	summary, err := generateSummary(ctx, d, transcript, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not generate summary: %v\n", err)
		return
	}

	fmt.Printf("\n%s\n", summary)
}
