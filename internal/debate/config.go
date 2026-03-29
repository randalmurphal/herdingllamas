package debate

import (
	"context"
	"fmt"
	"time"

	"github.com/randalmurphal/herdingllamas/internal/agent"
)

// Mode determines the conversation pattern and agent roles.
type Mode string

const (
	// ModeDebate is the default: two agents take opposing positions on a
	// question and argue toward resolution.
	ModeDebate Mode = "debate"

	// ModeExplore uses asymmetric cognitive roles: a Connector finds
	// analogies from unrelated domains, a Critic stress-tests them against
	// reality. Designed to resist convergence and surface unexpected ideas.
	ModeExplore Mode = "explore"

	// ModeInterrogate uses an Advocate/Interrogator pair for exhaustive plan
	// validation. The Advocate steel-mans the plan; the Interrogator
	// systematically probes every dimension for gaps. Designed to surface
	// implementation-level issues in a single session.
	ModeInterrogate Mode = "interrogate"
)

// Config configures a debate session.
type Config struct {
	Question    string        // The question to debate
	Models      []string      // Provider names: ["claude", "codex"]
	Mode        Mode          // Conversation mode (default: ModeDebate)
	WorkDir     string        // Working directory for agent sessions
	MaxTurns    int           // 0 = unlimited
	MaxDuration time.Duration // 0 = unlimited
	DBPath      string        // SQLite database path (empty = default)

	// Per-provider model and effort overrides. Keyed by provider name.
	// When empty, the provider's CLI default is used.
	ModelOverrides  map[string]string // e.g. {"claude": "opus", "codex": "gpt-5.4"}
	EffortOverrides map[string]string // e.g. {"claude": "max", "codex": "high"}
}

// Default model and effort settings per provider.
const (
	DefaultClaudeModel = "opus"
	DefaultCodexModel  = "" // Use codex CLI default
	DefaultClaudeEffort = "max"
	DefaultCodexEffort  = "xhigh"
)

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Models: []string{"claude", "codex"},
		Mode:   ModeDebate,
	}
}

// AgentMeta pairs a role name with its backing provider for display purposes.
type AgentMeta struct {
	Role     string // Display name: "proponent", "connector", etc.
	Provider string // Backing provider: "claude", "codex"
	Model    string // Model ID if overridden, empty for provider default
	Effort   string // Reasoning effort level if set
}

// RoleNames returns the two role names for the given mode.
// First return is agent 0's role, second is agent 1's role.
func RoleNames(mode Mode) (string, string) {
	switch mode {
	case ModeExplore:
		return "connector", "critic"
	case ModeInterrogate:
		return "advocate", "interrogator"
	default:
		return "proponent", "opponent"
	}
}

// ResolveConfig determines which providers to use, what models and effort
// levels to apply, based on explicit user selection and provider availability.
// Returns the resolved provider list (always length 2), agent metadata for
// display, and per-provider model/effort override maps for the engine config.
//
// Auto-detection behavior:
//   - Both available: use both (claude first, codex second)
//   - One available: use it for both agent slots
//   - None available: error
func ResolveConfig(ctx context.Context, opts ResolveOpts) (*ResolvedConfig, error) {
	role1, role2 := RoleNames(opts.Mode)

	var models []string
	if len(opts.Providers) > 0 {
		// Validate explicit selections.
		for _, m := range opts.Providers {
			status := agent.CheckProvider(ctx, agent.Provider(m))
			if !status.Installed {
				return nil, fmt.Errorf("%s CLI not found in PATH", m)
			}
			if !status.Authenticated {
				return nil, fmt.Errorf("%s is installed but not authenticated (run: %s auth login)", m, m)
			}
		}
		models = opts.Providers
		if len(models) == 1 {
			models = []string{models[0], models[0]}
		}
	} else {
		// Auto-detect available providers.
		available := agent.DetectProviders(ctx)
		switch len(available) {
		case 0:
			return nil, fmt.Errorf("no LLM providers found; install and authenticate the claude or codex CLI")
		case 1:
			p := string(available[0])
			models = []string{p, p}
		default:
			models = []string{string(available[0]), string(available[1])}
		}
	}

	// Build per-provider model and effort maps, applying defaults.
	modelOverrides := make(map[string]string)
	effortOverrides := make(map[string]string)

	for _, p := range models {
		if _, ok := modelOverrides[p]; !ok {
			modelOverrides[p] = defaultModel(agent.Provider(p), opts.ClaudeModel, opts.CodexModel)
			effortOverrides[p] = defaultEffort(agent.Provider(p), opts.ClaudeEffort, opts.CodexEffort)
		}
	}

	meta := []AgentMeta{
		{Role: role1, Provider: models[0], Model: modelOverrides[models[0]], Effort: effortOverrides[models[0]]},
		{Role: role2, Provider: models[1], Model: modelOverrides[models[1]], Effort: effortOverrides[models[1]]},
	}

	return &ResolvedConfig{
		Models:          models,
		AgentMeta:       meta,
		ModelOverrides:  modelOverrides,
		EffortOverrides: effortOverrides,
	}, nil
}

// ResolveOpts configures model/effort resolution.
type ResolveOpts struct {
	Mode      Mode
	Providers []string // Explicit provider selection; nil = auto-detect

	// Per-provider overrides. Empty = use defaults.
	ClaudeModel  string
	CodexModel   string
	ClaudeEffort string
	CodexEffort  string
}

// ResolvedConfig is the output of ResolveConfig.
type ResolvedConfig struct {
	Models          []string
	AgentMeta       []AgentMeta
	ModelOverrides  map[string]string
	EffortOverrides map[string]string
}

// defaultModel returns the model to use for a provider, preferring the
// explicit override, then the built-in default.
func defaultModel(p agent.Provider, claudeModel, codexModel string) string {
	switch p {
	case agent.ProviderClaude:
		if claudeModel != "" {
			return claudeModel
		}
		return DefaultClaudeModel
	case agent.ProviderCodex:
		if codexModel != "" {
			return codexModel
		}
		return DefaultCodexModel
	}
	return ""
}

// defaultEffort returns the effort level for a provider, preferring the
// explicit override, then the built-in default.
func defaultEffort(p agent.Provider, claudeEffort, codexEffort string) string {
	switch p {
	case agent.ProviderClaude:
		if claudeEffort != "" {
			return claudeEffort
		}
		return DefaultClaudeEffort
	case agent.ProviderCodex:
		if codexEffort != "" {
			return codexEffort
		}
		return DefaultCodexEffort
	}
	return ""
}
