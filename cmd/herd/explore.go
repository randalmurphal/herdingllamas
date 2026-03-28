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

func exploreCmd() *cobra.Command {
	var models []string
	var maxTurns int
	var maxDuration time.Duration
	var workDir string
	var jsonOutput bool
	var noSummary bool

	cmd := &cobra.Command{
		Use:   "explore [topic]",
		Short: "Explore a topic with lateral thinking + reality checking",
		Long: `Starts a collaborative exploration between two agents with asymmetric roles:

  Connector (first model): Searches unrelated domains for analogies, patterns,
  and structural similarities. Cannot research the topic directly.

  Critic (second model): Researches the topic directly and stress-tests the
  Connector's analogies against reality.

This mode resists convergence by giving agents different information access
and different cognitive tasks.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			topic := args[0]

			cfg := debate.Config{
				Question:    topic,
				Models:      models,
				Mode:        debate.ModeExplore,
				WorkDir:     workDir,
				MaxTurns:    maxTurns,
				MaxDuration: maxDuration,
			}

			if !jsonOutput {
				fmt.Println("Creating explore session...")
			}
			engine, err := debate.New(cfg)
			if err != nil {
				return fmt.Errorf("create explore engine: %w", err)
			}

			if !jsonOutput {
				fmt.Printf("Starting agents: %s (Connector) + %s (Critic)\n", models[0], models[1])
				fmt.Println("(This may take a moment while sessions initialize. Ctrl+C to abort.)")
			}
			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start explore: %w", err)
			}

			if jsonOutput {
				for range events {
				}
				engine.Stop()
				return outputDebateJSON(ctx, engine.DebateID(), models, !noSummary)
			}

			fmt.Println("Agents ready. Launching TUI...")
			m := tui.New(engine, events, topic)
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			engine.Stop()

			fmt.Printf("\nExploration %s saved to database.\n", engine.DebateID())

			if !noSummary {
				printDebateSummary(ctx, engine.DebateID())
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&models, "models", []string{"claude", "codex"}, "Models to use (first=Connector, second=Critic)")
	cmd.Flags().IntVar(&maxTurns, "max-turns", 0, "Maximum turns (0=unlimited)")
	cmd.Flags().DurationVar(&maxDuration, "max-duration", 0, "Maximum duration (0=unlimited)")
	cmd.Flags().StringVar(&workDir, "workdir", ".", "Working directory for agent sessions")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON (no TUI)")
	cmd.Flags().BoolVar(&noSummary, "no-summary", false, "Skip automatic summary after session ends")

	return cmd
}
