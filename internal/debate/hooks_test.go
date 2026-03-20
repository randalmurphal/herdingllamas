package debate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReadStateRoundTrip(t *testing.T) {
	debateID := "test-roundtrip-" + t.Name()
	t.Cleanup(func() { CleanupState(debateID) })

	original := DebateState{
		DebateID: debateID,
		Active:   true,
		Agents: map[string]AgentState{
			"claude": {UnreadCount: 3, Status: "running"},
			"codex":  {UnreadCount: 0, Status: "running"},
		},
	}

	if err := WriteState(debateID, original); err != nil {
		t.Fatalf("WriteState failed: %v", err)
	}

	got, err := ReadState(debateID)
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}

	if got.DebateID != original.DebateID {
		t.Errorf("DebateID: got %q, want %q", got.DebateID, original.DebateID)
	}
	if got.Active != original.Active {
		t.Errorf("Active: got %v, want %v", got.Active, original.Active)
	}
	if len(got.Agents) != len(original.Agents) {
		t.Fatalf("Agents count: got %d, want %d", len(got.Agents), len(original.Agents))
	}
	for name, wantAgent := range original.Agents {
		gotAgent, ok := got.Agents[name]
		if !ok {
			t.Errorf("agent %q missing from read state", name)
			continue
		}
		if gotAgent.UnreadCount != wantAgent.UnreadCount {
			t.Errorf("agent %q UnreadCount: got %d, want %d", name, gotAgent.UnreadCount, wantAgent.UnreadCount)
		}
		if gotAgent.Status != wantAgent.Status {
			t.Errorf("agent %q Status: got %q, want %q", name, gotAgent.Status, wantAgent.Status)
		}
	}
}

func TestReadStateMissingFile(t *testing.T) {
	_, err := ReadState("nonexistent-debate-id-12345")
	if err == nil {
		t.Fatal("expected error for missing state file, got nil")
	}
}

func TestStateDirCreatesDirectory(t *testing.T) {
	debateID := "test-statedir-" + t.Name()
	t.Cleanup(func() { CleanupState(debateID) })

	dir, err := StateDir(debateID)
	if err != nil {
		t.Fatalf("StateDir failed: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("state directory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("state path is not a directory: %s", dir)
	}

	expectedSuffix := filepath.Join(".herdingllamas", "debate-"+debateID)
	if !strings.HasSuffix(dir, expectedSuffix) {
		t.Errorf("state dir %q does not end with %q", dir, expectedSuffix)
	}
}

func TestCleanupStateRemovesDirectory(t *testing.T) {
	debateID := "test-cleanup-" + t.Name()

	// Create state so there's something to clean up.
	if err := WriteState(debateID, DebateState{DebateID: debateID}); err != nil {
		t.Fatalf("WriteState failed: %v", err)
	}

	dir, err := StateDir(debateID)
	if err != nil {
		t.Fatalf("StateDir failed: %v", err)
	}

	// Verify it exists before cleanup.
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("state directory should exist before cleanup: %v", err)
	}

	if err := CleanupState(debateID); err != nil {
		t.Fatalf("CleanupState failed: %v", err)
	}

	// Verify it's gone.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("state directory should not exist after cleanup, got err: %v", err)
	}
}

func TestGenerateStopHookScript(t *testing.T) {
	debateID := "test-hook-" + t.Name()

	script, err := GenerateStopHookScript(debateID)
	if err != nil {
		t.Fatalf("GenerateStopHookScript failed: %v", err)
	}

	// Verify the script has the expected structure.
	if !strings.HasPrefix(script, "#!/bin/sh") {
		t.Error("script should start with #!/bin/sh shebang")
	}
	if !strings.Contains(script, debateID) {
		t.Error("script should reference the debate ID")
	}
	if !strings.Contains(script, "state.json") {
		t.Error("script should reference state.json")
	}
	if !strings.Contains(script, "'decision'") {
		t.Error("script should contain decision output logic")
	}
	if !strings.Contains(script, "'continue'") {
		t.Error("script should contain continue output logic")
	}
	if !strings.Contains(script, "python3") {
		t.Error("script should use python3 for JSON parsing")
	}
}

func TestWriteStateOverwritesPrevious(t *testing.T) {
	debateID := "test-overwrite-" + t.Name()
	t.Cleanup(func() { CleanupState(debateID) })

	// Write initial state.
	initial := DebateState{
		DebateID: debateID,
		Active:   true,
		Agents: map[string]AgentState{
			"claude": {UnreadCount: 5, Status: "running"},
		},
	}
	if err := WriteState(debateID, initial); err != nil {
		t.Fatalf("first WriteState failed: %v", err)
	}

	// Overwrite with updated state.
	updated := DebateState{
		DebateID: debateID,
		Active:   false,
		Agents: map[string]AgentState{
			"claude": {UnreadCount: 0, Status: "stopped"},
		},
	}
	if err := WriteState(debateID, updated); err != nil {
		t.Fatalf("second WriteState failed: %v", err)
	}

	got, err := ReadState(debateID)
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}

	if got.Active != false {
		t.Errorf("Active should be false after overwrite, got true")
	}
	if got.Agents["claude"].Status != "stopped" {
		t.Errorf("agent status should be 'stopped', got %q", got.Agents["claude"].Status)
	}
	if got.Agents["claude"].UnreadCount != 0 {
		t.Errorf("unread count should be 0, got %d", got.Agents["claude"].UnreadCount)
	}
}
