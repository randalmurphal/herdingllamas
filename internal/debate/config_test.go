package debate

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if len(cfg.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(cfg.Models))
	}
	if cfg.Models[0] != "claude" {
		t.Errorf("expected first model to be 'claude', got %q", cfg.Models[0])
	}
	if cfg.Models[1] != "codex" {
		t.Errorf("expected second model to be 'codex', got %q", cfg.Models[1])
	}
	if cfg.NudgeInterval != 30*time.Second {
		t.Errorf("expected NudgeInterval to be 30s, got %v", cfg.NudgeInterval)
	}
	if cfg.MaxTurns != 0 {
		t.Errorf("expected MaxTurns to be 0 (unlimited), got %d", cfg.MaxTurns)
	}
	if cfg.MaxDuration != 0 {
		t.Errorf("expected MaxDuration to be 0 (unlimited), got %v", cfg.MaxDuration)
	}
	if cfg.MaxBudgetUSD != 0 {
		t.Errorf("expected MaxBudgetUSD to be 0 (unlimited), got %f", cfg.MaxBudgetUSD)
	}
	if cfg.Question != "" {
		t.Errorf("expected Question to be empty, got %q", cfg.Question)
	}
	if cfg.WorkDir != "" {
		t.Errorf("expected WorkDir to be empty, got %q", cfg.WorkDir)
	}
	if cfg.DBPath != "" {
		t.Errorf("expected DBPath to be empty, got %q", cfg.DBPath)
	}
}
