package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/randalmurphal/herdingllamas/internal/debate"
	"github.com/randalmurphal/herdingllamas/internal/tui"
)

func debateCmd() *cobra.Command {
	var models []string
	var maxTurns int
	var maxDuration time.Duration
	var maxBudget float64
	var workDir string

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
				Question:     question,
				Models:       models,
				WorkDir:      workDir,
				MaxTurns:     maxTurns,
				MaxDuration:  maxDuration,
				MaxBudgetUSD: maxBudget,
			}

			engine, err := debate.New(cfg)
			if err != nil {
				return fmt.Errorf("create debate engine: %w", err)
			}

			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start debate: %w", err)
			}

			m := tui.New(engine, events, question)
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			// Ensure debate is stopped when TUI exits.
			engine.Stop()

			fmt.Printf("\nDebate %s saved to database.\n", engine.DebateID())
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&models, "models", []string{"claude", "codex"}, "Models to debate (comma-separated)")
	cmd.Flags().IntVar(&maxTurns, "max-turns", 0, "Maximum debate turns (0=unlimited)")
	cmd.Flags().DurationVar(&maxDuration, "max-duration", 0, "Maximum debate duration (0=unlimited)")
	cmd.Flags().Float64Var(&maxBudget, "max-budget", 0, "Maximum budget in USD (0=unlimited)")
	cmd.Flags().StringVar(&workDir, "workdir", ".", "Working directory for agent sessions")

	return cmd
}
