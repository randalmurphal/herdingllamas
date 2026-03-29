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

func interrogateCmd() *cobra.Command {
	var models []string
	var maxTurns int
	var maxDuration time.Duration
	var workDir string
	var jsonOutput bool
	var noSummary bool

	cmd := &cobra.Command{
		Use:   "interrogate [plan description or question]",
		Short: "Interrogate a plan with systematic gap analysis",
		Long: `Starts a plan interrogation between two agents with asymmetric roles:

  Advocate (first model): Deeply understands the plan and defends it with
  evidence from the codebase and research. Confirms gaps when they can't
  be defended.

  Interrogator (second model): Systematically probes the plan across every
  dimension — assumptions, data flow, integration boundaries, failure modes,
  state, dependencies, operations, performance, sequencing, and ambiguity.

This mode resists premature convergence by requiring the Interrogator to
work through a structured dimension checklist before concluding. Designed
to surface implementation-level gaps in a single session.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			question := args[0]

			cfg := debate.Config{
				Question:    question,
				Models:      models,
				Mode:        debate.ModeInterrogate,
				WorkDir:     workDir,
				MaxTurns:    maxTurns,
				MaxDuration: maxDuration,
			}

			if !jsonOutput {
				fmt.Println("Creating interrogation session...")
			}
			engine, err := debate.New(cfg)
			if err != nil {
				return fmt.Errorf("create interrogation engine: %w", err)
			}

			if !jsonOutput {
				fmt.Printf("Starting agents: %s (Advocate) + %s (Interrogator)\n", models[0], models[1])
				fmt.Println("(This may take a moment while sessions initialize. Ctrl+C to abort.)")
			}
			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start interrogation: %w", err)
			}

			if jsonOutput {
				for range events {
				}
				engine.Stop()
				return outputDebateJSON(ctx, engine.DebateID(), models, !noSummary)
			}

			fmt.Println("Agents ready. Launching TUI...")
			m := tui.New(engine, events, question)
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			engine.Stop()

			fmt.Printf("\nInterrogation %s saved to database.\n", engine.DebateID())

			if !noSummary {
				printDebateSummary(ctx, engine.DebateID())
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&models, "models", []string{"claude", "codex"}, "Models to use (first=Advocate, second=Interrogator)")
	cmd.Flags().IntVar(&maxTurns, "max-turns", 0, "Maximum turns (0=unlimited)")
	cmd.Flags().DurationVar(&maxDuration, "max-duration", 0, "Maximum duration (0=unlimited)")
	cmd.Flags().StringVar(&workDir, "workdir", ".", "Working directory for agent sessions")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON (no TUI)")
	cmd.Flags().BoolVar(&noSummary, "no-summary", false, "Skip automatic summary after session ends")

	return cmd
}
