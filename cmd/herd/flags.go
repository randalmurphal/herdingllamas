package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/randalmurphal/herdingllamas/internal/debate"
)

// commonFlags holds flags shared across debate, explore, and interrogate commands.
type commonFlags struct {
	models      []string
	claudeModel string
	codexModel  string
	claudeEffort string
	codexEffort  string
	maxTurns    int
	maxDuration time.Duration
	workDir     string
	jsonOutput  bool
	noSummary   bool
}

// registerCommonFlags adds the shared flag set to a cobra command.
func registerCommonFlags(cmd *cobra.Command, f *commonFlags) {
	cmd.Flags().StringSliceVar(&f.models, "models", nil, "Providers to use (e.g. claude,codex); auto-detects if omitted")
	cmd.Flags().StringVar(&f.claudeModel, "claude-model", "", "Claude model (e.g. opus, sonnet); default: opus")
	cmd.Flags().StringVar(&f.codexModel, "codex-model", "", "Codex model (e.g. gpt-5.4, o3); default: provider default")
	cmd.Flags().StringVar(&f.claudeEffort, "claude-effort", "", "Claude reasoning effort (low, medium, high, max); default: max")
	cmd.Flags().StringVar(&f.codexEffort, "codex-effort", "", "Codex reasoning effort (minimal, low, medium, high, xhigh); default: xhigh")
	cmd.Flags().IntVar(&f.maxTurns, "max-turns", 0, "Maximum turns (0=unlimited)")
	cmd.Flags().DurationVar(&f.maxDuration, "max-duration", 0, "Maximum duration (0=unlimited)")
	cmd.Flags().StringVar(&f.workDir, "workdir", ".", "Working directory for agent sessions")
	cmd.Flags().BoolVar(&f.jsonOutput, "json", false, "Output results as JSON (no TUI)")
	cmd.Flags().BoolVar(&f.noSummary, "no-summary", false, "Skip automatic summary after session ends")
}

// resolveFromFlags calls debate.ResolveConfig using the common flags.
func resolveFromFlags(ctx context.Context, cmd *cobra.Command, f commonFlags, mode debate.Mode) (*debate.ResolvedConfig, error) {
	var explicit []string
	if cmd.Flags().Changed("models") {
		explicit = f.models
	}

	return debate.ResolveConfig(ctx, debate.ResolveOpts{
		Mode:         mode,
		Providers:    explicit,
		ClaudeModel:  f.claudeModel,
		CodexModel:   f.codexModel,
		ClaudeEffort: f.claudeEffort,
		CodexEffort:  f.codexEffort,
	})
}

// printAgentTable prints the resolved agent configuration.
func printAgentTable(meta []debate.AgentMeta) {
	for _, am := range meta {
		model := am.Model
		if model == "" {
			model = "default"
		}
		effort := am.Effort
		if effort == "" {
			effort = "default"
		}
		fmt.Printf("  %s → %s (model: %s, effort: %s)\n", am.Role, am.Provider, model, effort)
	}
}
