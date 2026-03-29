package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/randalmurphal/herdingllamas/internal/debate"
	"github.com/randalmurphal/herdingllamas/internal/tui"
)

func interrogateCmd() *cobra.Command {
	var flags commonFlags

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

			rc, err := resolveFromFlags(ctx, cmd, flags, debate.ModeInterrogate)
			if err != nil {
				return err
			}

			cfg := debate.Config{
				Question:        question,
				Models:          rc.Models,
				Mode:            debate.ModeInterrogate,
				WorkDir:         flags.workDir,
				MaxTurns:        flags.maxTurns,
				MaxDuration:     flags.maxDuration,
				ModelOverrides:  rc.ModelOverrides,
				EffortOverrides: rc.EffortOverrides,
			}

			if !flags.jsonOutput {
				fmt.Println("Creating interrogation session...")
			}
			engine, err := debate.New(cfg, rc.AgentMeta)
			if err != nil {
				return fmt.Errorf("create interrogation engine: %w", err)
			}

			if !flags.jsonOutput {
				printAgentTable(rc.AgentMeta)
				fmt.Println("(This may take a moment while sessions initialize. Ctrl+C to abort.)")
			}
			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start interrogation: %w", err)
			}

			if flags.jsonOutput {
				for range events {
				}
				engine.Stop()
				return outputDebateJSON(ctx, engine.DebateID(), rc.AgentMeta, !flags.noSummary)
			}

			fmt.Println("Agents ready. Launching TUI...")
			providers := agentMetaToProviders(rc.AgentMeta)
			m := tui.New(engine, events, question, providers, agentMetaOrder(rc.AgentMeta))
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			engine.Stop()
			fmt.Printf("\nInterrogation %s saved to database.\n", engine.DebateID())

			if !flags.noSummary {
				printDebateSummary(ctx, engine.DebateID())
			}

			return nil
		},
	}

	registerCommonFlags(cmd, &flags)
	return cmd
}
