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

func codeReviewCmd() *cobra.Command {
	var flags commonFlags

	cmd := &cobra.Command{
		Use:   "code-review [what to review]",
		Short: "Dual-perspective code review with cross-examination",
		Long: `Starts a code review between two agents with deliberately different perspectives:

  Scrutinizer (first model): Works from the diff outward — reads the changed
  code, traces into callers and dependencies, reviews for correctness, safety,
  edge cases, failure modes, concurrency, and test coverage.

  Defender (second model): Works from the system inward — reads the tests,
  callers, and adjacent code first, then reviews the diff for architectural
  fit, design intent, maintainability, integration, and backwards compatibility.

Both agents complete independent reviews before reading each other's findings.
They then cross-examine: challenging false positives, confirming real issues,
escalating findings the other underestimated, and identifying dimensions
neither covered. The final summary tracks provenance — what was found
independently, what survived challenge, and what emerged only during discussion.

The argument describes what to review: a branch name, file paths, a PR
description, or any other scoping instruction. The agents have full tool
access and will use git, file reads, and web search to investigate.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			question := args[0]

			rc, err := resolveFromFlags(ctx, cmd, flags, debate.ModeCodeReview)
			if err != nil {
				return err
			}

			cfg := debate.Config{
				Question:        question,
				Models:          rc.Models,
				Mode:            debate.ModeCodeReview,
				WorkDir:         flags.workDir,
				MaxTurns:        flags.maxTurns,
				MaxDuration:     flags.maxDuration,
				ModelOverrides:  rc.ModelOverrides,
				EffortOverrides: rc.EffortOverrides,
			}

			if !flags.jsonOutput {
				fmt.Println("Creating code review session...")
			}
			engine, err := debate.New(cfg, rc.AgentMeta)
			if err != nil {
				return fmt.Errorf("create review engine: %w", err)
			}

			if !flags.jsonOutput {
				printAgentTable(rc.AgentMeta)
				fmt.Println("(This may take a moment while sessions initialize. Ctrl+C to abort.)")
			}
			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start review: %w", err)
			}

			if flags.jsonOutput {
				for range events {
				}
				engine.Stop()
				return outputDebateJSON(ctx, engine.DebateID(), rc.AgentMeta, !flags.noSummary)
			}

			fmt.Println("Agents ready. Launching review TUI...")
			providers := agentMetaToProviders(rc.AgentMeta)
			m := tui.New(engine, events, question, providers, agentMetaOrder(rc.AgentMeta))
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			engine.Stop()
			fmt.Printf("\nCode review %s saved to database.\n", engine.DebateID())

			if !flags.noSummary {
				printDebateSummary(ctx, engine.DebateID())
			}

			return nil
		},
	}

	registerCommonFlags(cmd, &flags)
	return cmd
}
