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

func exploreCmd() *cobra.Command {
	var flags commonFlags

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

			rc, err := resolveFromFlags(ctx, cmd, flags, debate.ModeExplore)
			if err != nil {
				return err
			}

			cfg := debate.Config{
				Question:        topic,
				Models:          rc.Models,
				Mode:            debate.ModeExplore,
				WorkDir:         flags.workDir,
				MaxTurns:        flags.maxTurns,
				MaxDuration:     flags.maxDuration,
				ModelOverrides:  rc.ModelOverrides,
				EffortOverrides: rc.EffortOverrides,
			}

			if !flags.jsonOutput {
				fmt.Println("Creating explore session...")
			}
			engine, err := debate.New(cfg, rc.AgentMeta)
			if err != nil {
				return fmt.Errorf("create explore engine: %w", err)
			}

			if !flags.jsonOutput {
				printAgentTable(rc.AgentMeta)
				fmt.Println("(This may take a moment while sessions initialize. Ctrl+C to abort.)")
			}
			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start explore: %w", err)
			}

			if flags.jsonOutput {
				for range events {
				}
				engine.Stop()
				return outputDebateJSON(ctx, engine.DebateID(), rc.AgentMeta, !flags.noSummary)
			}

			fmt.Println("Agents ready. Launching TUI...")
			providers := agentMetaToProviders(rc.AgentMeta)
			m := tui.New(engine, events, topic, providers, agentMetaOrder(rc.AgentMeta))
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			engine.Stop()
			fmt.Printf("\nExploration %s saved to database.\n", engine.DebateID())

			if !flags.noSummary {
				printDebateSummary(ctx, engine.DebateID())
			}

			return nil
		},
	}

	registerCommonFlags(cmd, &flags)
	return cmd
}
