package debate

import "time"

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
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Models: []string{"claude", "codex"},
		Mode:   ModeDebate,
	}
}
