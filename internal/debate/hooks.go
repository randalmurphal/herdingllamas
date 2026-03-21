package debate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DebateState is written to a state file that hook scripts read to decide
// whether an agent should be allowed to stop.
type DebateState struct {
	DebateID string                `json:"debate_id"`
	Active   bool                  `json:"active"`
	Agents   map[string]AgentState `json:"agents"`
}

// AgentState tracks per-agent state for the hook script.
type AgentState struct {
	UnreadCount int    `json:"unread_count"`
	Status      string `json:"status"` // "running", "stopped", "error"
}

// StateDir returns the state directory path for a debate: ~/.herdingllamas/debate-{id}/
// Creates the directory if it does not exist.
func StateDir(debateID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}

	dir := filepath.Join(home, ".herdingllamas", fmt.Sprintf("debate-%s", debateID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating state directory %s: %w", dir, err)
	}
	return dir, nil
}

// stateFilePath returns the path to the state JSON file without creating the directory.
func stateFilePath(debateID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".herdingllamas", fmt.Sprintf("debate-%s", debateID), "state.json"), nil
}

// WriteState writes the current debate state for hook scripts to read.
func WriteState(debateID string, state DebateState) error {
	dir, err := StateDir(debateID)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling debate state: %w", err)
	}

	statePath := filepath.Join(dir, "state.json")

	// Write atomically: write to temp file then rename. This prevents hook
	// scripts from reading a partially written file.
	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing state file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, statePath); err != nil {
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

// ReadState reads the debate state from disk.
func ReadState(debateID string) (*DebateState, error) {
	statePath, err := stateFilePath(debateID)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, fmt.Errorf("reading state file %s: %w", statePath, err)
	}

	var state DebateState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file %s: %w", statePath, err)
	}
	return &state, nil
}

// GenerateStopHookScript returns a shell script that checks the debate state
// and returns decision:"block" if the debate is still active with unread
// messages or if the opponent is still running.
//
// The script is designed to be used as a Claude Code or Codex stop hook command.
// It reads hook input JSON from stdin, reads the state file, and outputs a
// JSON decision to stdout.
func GenerateStopHookScript(debateID string) (string, error) {
	for _, c := range debateID {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
			return "", fmt.Errorf("invalid debate ID: contains unsafe characters")
		}
	}

	statePath, err := stateFilePath(debateID)
	if err != nil {
		return "", err
	}

	// The script uses python3 as a portable JSON parser since jq may not be
	// installed everywhere. The python3 fallback is essentially universal on
	// macOS and most Linux distributions.
	script := fmt.Sprintf(`#!/bin/sh
# HerdingLlamas stop hook for debate %s
# This script blocks agent stop attempts while the debate is active.

STATE_FILE="%s"

# Read stdin (hook input JSON) but we don't need it for the decision.
cat > /dev/null

if [ ! -f "$STATE_FILE" ]; then
  # No state file means debate isn't managed; allow stop.
  printf '{"continue":true}\n'
  exit 0
fi

# Parse state file and make decision using python3.
python3 -c "
import json, sys

with open('$STATE_FILE') as f:
    state = json.load(f)

if not state.get('active', False):
    print(json.dumps({'continue': True}))
    sys.exit(0)

agents = state.get('agents', {})

# Find this agent's unread count and opponent status.
# The hook script is invoked per-agent, so we check all agents.
# An agent should stay if it has unread messages or if any opponent is running.
for name, info in agents.items():
    unread = info.get('unread_count', 0)
    if unread > 0:
        msg = f'Debate active. {unread} unread message(s) for {name}.'
        print(json.dumps({'decision': 'block', 'reason': msg}))
        sys.exit(0)

# Check if any agent is still running (opponent may be composing).
running = [n for n, i in agents.items() if i.get('status') == 'running']
if len(running) > 0:
    msg = 'Waiting for opponent response.'
    print(json.dumps({'decision': 'block', 'reason': msg}))
    sys.exit(0)

# All clear.
print(json.dumps({'continue': True}))
" 2>/dev/null

# If python3 fails for any reason, allow stop rather than blocking forever.
if [ $? -ne 0 ]; then
  printf '{"continue":true}\n'
fi

exit 0
`, debateID, statePath)

	return script, nil
}

// CleanupState removes the state directory for a debate.
func CleanupState(debateID string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	dir := filepath.Join(home, ".herdingllamas", fmt.Sprintf("debate-%s", debateID))
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing state directory %s: %w", dir, err)
	}
	return nil
}
