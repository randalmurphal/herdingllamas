package debate

import "time"

// Config configures a debate session.
type Config struct {
	Question      string        // The question to debate
	Models        []string      // Provider names: ["claude", "codex"]
	WorkDir       string        // Working directory for agent sessions
	MaxTurns      int           // 0 = unlimited
	MaxDuration   time.Duration // 0 = unlimited
	MaxBudgetUSD  float64       // 0 = unlimited
	NudgeInterval time.Duration // How often agents are nudged to respond
	DBPath        string        // SQLite database path (empty = default)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Models:        []string{"claude", "codex"},
		NudgeInterval: 30 * time.Second,
	}
}
