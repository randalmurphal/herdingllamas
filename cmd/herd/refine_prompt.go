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

func refinePromptCmd() *cobra.Command {
	var flags commonFlags
	var targetModel string

	cmd := &cobra.Command{
		Use:   "refine-prompt [prompt text or file path]",
		Short: "Refine a prompt with systematic evaluation and improvement",
		Long: `Starts a prompt refinement session between two agents with asymmetric roles:

  Evaluator (first model): Systematically assesses the prompt against prompt
  engineering principles — clarity, specificity, structure, framing, and more.
  Quotes exact text and rates severity of each finding.

  Refiner (second model): Defends intentional design choices and proposes
  concrete before/after text replacements for valid findings. Pushes back
  on over-engineering.

The argument can be a file path (the file contents will be read) or literal
prompt text. Use --target to specify which model the prompt is designed for.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Read prompt from file if the argument is a readable file path,
			// otherwise treat it as literal prompt text.
			promptText := args[0]
			if data, err := os.ReadFile(promptText); err == nil {
				promptText = string(data)
			}

			rc, err := resolveFromFlags(ctx, cmd, flags, debate.ModeRefinePrompt)
			if err != nil {
				return err
			}

			cfg := debate.Config{
				Question:        promptText,
				Models:          rc.Models,
				Mode:            debate.ModeRefinePrompt,
				WorkDir:         flags.workDir,
				MaxTurns:        flags.maxTurns,
				MaxDuration:     flags.maxDuration,
				TargetModel:     targetModel,
				ModelOverrides:  rc.ModelOverrides,
				EffortOverrides: rc.EffortOverrides,
			}

			if !flags.jsonOutput {
				fmt.Println("Creating prompt refinement session...")
			}
			engine, err := debate.New(cfg, rc.AgentMeta)
			if err != nil {
				return fmt.Errorf("create refinement engine: %w", err)
			}

			if !flags.jsonOutput {
				printAgentTable(rc.AgentMeta)
				fmt.Println("(This may take a moment while sessions initialize. Ctrl+C to abort.)")
			}
			events, err := engine.Start(ctx)
			if err != nil {
				engine.Stop()
				return fmt.Errorf("start refinement: %w", err)
			}

			if flags.jsonOutput {
				for range events {
				}
				engine.Stop()
				return outputDebateJSON(ctx, engine.DebateID(), rc.AgentMeta, !flags.noSummary)
			}

			fmt.Println("Agents ready. Launching TUI...")
			displayQuestion := promptText
			if len(displayQuestion) > 120 {
				displayQuestion = displayQuestion[:117] + "..."
			}
			providers := agentMetaToProviders(rc.AgentMeta)
			m := tui.New(engine, events, displayQuestion, providers, agentMetaOrder(rc.AgentMeta))
			p := tea.NewProgram(m, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				engine.Stop()
				return fmt.Errorf("TUI error: %w", err)
			}

			engine.Stop()
			fmt.Printf("\nRefinement session %s saved to database.\n", engine.DebateID())

			if !flags.noSummary {
				printDebateSummary(ctx, engine.DebateID())
			}

			return nil
		},
	}

	registerCommonFlags(cmd, &flags)
	cmd.Flags().StringVar(&targetModel, "target", "", "Target model the prompt is designed for (e.g. claude, gpt-5, gemini)")
	return cmd
}
