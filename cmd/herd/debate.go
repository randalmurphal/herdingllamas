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
	DebateID  string        `json:"debate_id"`
	Question  string        `json:"question"`
	Models    []string      `json:"models"`
	Status    string        `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   *time.Time    `json:"ended_at,omitempty"`
	Summary   string        `json:"summary,omitempty"`
	Messages  []messageJSON `json:"messages"`
}

type messageJSON struct {
	Turn      int       `json:"turn"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

func debateCmd() *cobra.Command {
	var models []string
	var maxTurns int
	var maxDuration time.Duration
	var workDir string
	var jsonOutput bool
	var noSummary bool

	cmd := &cobra.Command{
		Use:   "debate [question]",
		Short: "Start a multi-model debate",
		Long:  "Starts an interactive debate between LLM agents on the given question.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			question := args[0]

			cfg := debate.Config{
				Question:    question,
				Models:      models,
				WorkDir:     workDir,
				MaxTurns:    maxTurns,
				MaxDuration: maxDuration,
			}

			if !jsonOutput {
				fmt.Println("Creating debate engine...")
			}
			engine, err := debate.New(cfg)
			if err != nil {
				return fmt.Errorf("create debate engine: %w", err)
			}

			if !jsonOutput {
				fmt.Printf("Starting agents: %v\n", models)
				fmt.Println("(This may take a moment while sessions initialize. Ctrl+C to abort.)")
			}
			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start debate: %w", err)
			}

			if jsonOutput {
				// Drain events without TUI until the debate ends.
				for range events {
				}
				engine.Stop()
				return outputDebateJSON(ctx, engine.DebateID(), models, !noSummary)
			}

			fmt.Println("Agents ready. Launching debate TUI...")
			m := tui.New(engine, events, question)
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			// Ensure debate is stopped when TUI exits.
			engine.Stop()

			fmt.Printf("\nDebate %s saved to database.\n", engine.DebateID())

			if !noSummary {
				printDebateSummary(ctx, engine.DebateID())
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&models, "models", []string{"claude", "codex"}, "Models to debate (comma-separated)")
	cmd.Flags().IntVar(&maxTurns, "max-turns", 0, "Maximum debate turns (0=unlimited)")
	cmd.Flags().DurationVar(&maxDuration, "max-duration", 0, "Maximum debate duration (0=unlimited)")
	cmd.Flags().StringVar(&workDir, "workdir", ".", "Working directory for agent sessions")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output debate results as JSON (no TUI)")
	cmd.Flags().BoolVar(&noSummary, "no-summary", false, "Skip automatic summary after debate ends")

	return cmd
}

// outputDebateJSON writes the completed debate as JSON to stdout.
func outputDebateJSON(ctx context.Context, debateID string, models []string, includeSummary bool) error {
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

	out := debateJSON{
		DebateID:  d.ID,
		Question:  d.Question,
		Models:    models,
		Status:    d.Status,
		StartedAt: d.CreatedAt,
		EndedAt:   d.EndedAt,
		Messages:  make([]messageJSON, 0, len(messages)),
	}

	for _, msg := range messages {
		out.Messages = append(out.Messages, messageJSON{
			Turn:      msg.TurnNum,
			Author:    msg.Author,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}

	if includeSummary {
		transcript := formatTranscript(d, messages)
		summary, err := generateSummary(ctx, d, transcript)
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
	summary, err := generateSummary(ctx, d, transcript)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not generate summary: %v\n", err)
		return
	}

	fmt.Printf("\n%s\n", summary)
}
