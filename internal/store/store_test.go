package store

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func mustOpen(t *testing.T) *Store {
	t.Helper()
	s, err := OpenInMemory()
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenClose(t *testing.T) {
	s, err := OpenInMemory()
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestDefaultDBPath(t *testing.T) {
	p, err := DefaultDBPath()
	if err != nil {
		t.Fatalf("DefaultDBPath: %v", err)
	}
	if p == "" {
		t.Fatal("DefaultDBPath returned empty string")
	}
}

func TestInsertAndGetDebate(t *testing.T) {
	s := mustOpen(t)
	now := time.Now().UTC().Truncate(time.Second)

	d := Debate{
		ID:        uuid.New().String(),
		Question:  "Is Go better than Rust?",
		Config:    `{"turns": 5}`,
		Status:    "active",
		CreatedAt: now,
	}

	if err := s.InsertDebate(d); err != nil {
		t.Fatalf("InsertDebate: %v", err)
	}

	got, err := s.GetDebate(d.ID)
	if err != nil {
		t.Fatalf("GetDebate: %v", err)
	}
	if got == nil {
		t.Fatal("GetDebate returned nil")
	}
	if got.ID != d.ID {
		t.Errorf("ID = %q, want %q", got.ID, d.ID)
	}
	if got.Question != d.Question {
		t.Errorf("Question = %q, want %q", got.Question, d.Question)
	}
	if got.Config != d.Config {
		t.Errorf("Config = %q, want %q", got.Config, d.Config)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}
	if got.EndedAt != nil {
		t.Errorf("EndedAt = %v, want nil", got.EndedAt)
	}
}

func TestGetDebateNotFound(t *testing.T) {
	s := mustOpen(t)

	got, err := s.GetDebate("nonexistent")
	if err != nil {
		t.Fatalf("GetDebate: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent debate, got %+v", got)
	}
}

func TestUpdateDebateStatus(t *testing.T) {
	s := mustOpen(t)

	d := Debate{
		ID:        uuid.New().String(),
		Question:  "test",
		Config:    "{}",
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.InsertDebate(d); err != nil {
		t.Fatalf("InsertDebate: %v", err)
	}

	if err := s.UpdateDebateStatus(d.ID, "completed"); err != nil {
		t.Fatalf("UpdateDebateStatus: %v", err)
	}

	got, err := s.GetDebate(d.ID)
	if err != nil {
		t.Fatalf("GetDebate: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want %q", got.Status, "completed")
	}
}

func TestUpdateDebateStatusNotFound(t *testing.T) {
	s := mustOpen(t)

	err := s.UpdateDebateStatus("nonexistent", "completed")
	if err == nil {
		t.Fatal("expected error for nonexistent debate, got nil")
	}
}

func TestEndDebate(t *testing.T) {
	s := mustOpen(t)

	d := Debate{
		ID:        uuid.New().String(),
		Question:  "test",
		Config:    "{}",
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.InsertDebate(d); err != nil {
		t.Fatalf("InsertDebate: %v", err)
	}

	if err := s.EndDebate(d.ID, "completed"); err != nil {
		t.Fatalf("EndDebate: %v", err)
	}

	got, err := s.GetDebate(d.ID)
	if err != nil {
		t.Fatalf("GetDebate: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want %q", got.Status, "completed")
	}
	if got.EndedAt == nil {
		t.Fatal("EndedAt should not be nil after EndDebate")
	}
}

func TestListDebates(t *testing.T) {
	s := mustOpen(t)

	// Insert debates with different timestamps to verify ordering.
	earlier := time.Now().UTC().Add(-time.Hour)
	later := time.Now().UTC()

	d1 := Debate{ID: uuid.New().String(), Question: "first", Config: "{}", Status: "active", CreatedAt: earlier}
	d2 := Debate{ID: uuid.New().String(), Question: "second", Config: "{}", Status: "active", CreatedAt: later}

	if err := s.InsertDebate(d1); err != nil {
		t.Fatalf("InsertDebate d1: %v", err)
	}
	if err := s.InsertDebate(d2); err != nil {
		t.Fatalf("InsertDebate d2: %v", err)
	}

	debates, err := s.ListDebates()
	if err != nil {
		t.Fatalf("ListDebates: %v", err)
	}
	if len(debates) != 2 {
		t.Fatalf("len = %d, want 2", len(debates))
	}
	// Newest first.
	if debates[0].ID != d2.ID {
		t.Errorf("first debate should be d2 (newer), got %q", debates[0].Question)
	}
	if debates[1].ID != d1.ID {
		t.Errorf("second debate should be d1 (older), got %q", debates[1].Question)
	}
}

// --- Messages ---

func TestInsertAndGetMessages(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	now := time.Now().UTC()
	msgs := []Message{
		{ID: uuid.New().String(), DebateID: debateID, Author: "claude", Content: "Hello", Timestamp: now, TurnNum: 0},
		{ID: uuid.New().String(), DebateID: debateID, Author: "codex", Content: "Hi there", Timestamp: now.Add(time.Second), TurnNum: 1},
		{ID: uuid.New().String(), DebateID: debateID, Author: "claude", Content: "Let's debate", Timestamp: now.Add(2 * time.Second), TurnNum: 2},
	}

	for _, m := range msgs {
		if err := s.InsertMessage(m); err != nil {
			t.Fatalf("InsertMessage: %v", err)
		}
	}

	got, err := s.GetDebateMessages(debateID)
	if err != nil {
		t.Fatalf("GetDebateMessages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	// Verify ordering by turn_num.
	for i, m := range got {
		if m.TurnNum != i {
			t.Errorf("message %d: TurnNum = %d, want %d", i, m.TurnNum, i)
		}
	}
	if got[0].Author != "claude" || got[1].Author != "codex" || got[2].Author != "claude" {
		t.Errorf("unexpected author ordering: %s, %s, %s", got[0].Author, got[1].Author, got[2].Author)
	}
}

func TestGetMessageCount(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	count, err := s.GetMessageCount(debateID)
	if err != nil {
		t.Fatalf("GetMessageCount: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	for i := 0; i < 5; i++ {
		m := Message{
			ID:        uuid.New().String(),
			DebateID:  debateID,
			Author:    "claude",
			Content:   "msg",
			Timestamp: time.Now().UTC(),
			TurnNum:   i,
		}
		if err := s.InsertMessage(m); err != nil {
			t.Fatalf("InsertMessage %d: %v", i, err)
		}
	}

	count, err = s.GetMessageCount(debateID)
	if err != nil {
		t.Fatalf("GetMessageCount: %v", err)
	}
	if count != 5 {
		t.Errorf("count = %d, want 5", count)
	}
}

// --- Agents ---

func TestInsertAndGetAgents(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)
	now := time.Now().UTC()

	a := Agent{
		ID:        uuid.New().String(),
		DebateID:  debateID,
		Name:      "claude",
		Provider:  "anthropic",
		Model:     "claude-4",
		SessionID: "sess-123",
		Status:    "starting",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.InsertAgent(a); err != nil {
		t.Fatalf("InsertAgent: %v", err)
	}

	agents, err := s.GetDebateAgents(debateID)
	if err != nil {
		t.Fatalf("GetDebateAgents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len = %d, want 1", len(agents))
	}

	got := agents[0]
	if got.Name != "claude" {
		t.Errorf("Name = %q, want %q", got.Name, "claude")
	}
	if got.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", got.Provider, "anthropic")
	}
	if got.Status != "starting" {
		t.Errorf("Status = %q, want %q", got.Status, "starting")
	}
}

func TestUpdateAgentStatus(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)
	now := time.Now().UTC()

	a := Agent{
		ID:        uuid.New().String(),
		DebateID:  debateID,
		Name:      "codex",
		Provider:  "openai",
		Status:    "starting",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.InsertAgent(a); err != nil {
		t.Fatalf("InsertAgent: %v", err)
	}

	if err := s.UpdateAgentStatus(a.ID, "active"); err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}

	agents, err := s.GetDebateAgents(debateID)
	if err != nil {
		t.Fatalf("GetDebateAgents: %v", err)
	}
	if agents[0].Status != "active" {
		t.Errorf("Status = %q, want %q", agents[0].Status, "active")
	}
	if !agents[0].UpdatedAt.After(now.Add(-time.Second)) {
		t.Error("UpdatedAt should have been refreshed")
	}
}

func TestUpdateAgentStatusNotFound(t *testing.T) {
	s := mustOpen(t)

	err := s.UpdateAgentStatus("nonexistent", "active")
	if err == nil {
		t.Fatal("expected error for nonexistent agent, got nil")
	}
}

func TestUpdateAgentUsage(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)
	now := time.Now().UTC()

	a := Agent{
		ID:        uuid.New().String(),
		DebateID:  debateID,
		Name:      "claude",
		Provider:  "anthropic",
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.InsertAgent(a); err != nil {
		t.Fatalf("InsertAgent: %v", err)
	}

	if err := s.UpdateAgentUsage(a.ID, 1000, 500, 0.05); err != nil {
		t.Fatalf("UpdateAgentUsage: %v", err)
	}

	agents, err := s.GetDebateAgents(debateID)
	if err != nil {
		t.Fatalf("GetDebateAgents: %v", err)
	}
	got := agents[0]
	if got.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", got.InputTokens)
	}
	if got.OutputTokens != 500 {
		t.Errorf("OutputTokens = %d, want 500", got.OutputTokens)
	}
	if got.CostUSD != 0.05 {
		t.Errorf("CostUSD = %f, want 0.05", got.CostUSD)
	}
}

func TestUpdateAgentUsageNotFound(t *testing.T) {
	s := mustOpen(t)

	err := s.UpdateAgentUsage("nonexistent", 100, 50, 0.01)
	if err == nil {
		t.Fatal("expected error for nonexistent agent, got nil")
	}
}

// --- Events ---

func TestInsertAndGetEvents(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)
	now := time.Now().UTC()

	events := []Event{
		{DebateID: debateID, EventType: "agent_started", AgentName: "claude", Payload: `{"model":"claude-4"}`, Timestamp: now},
		{DebateID: debateID, EventType: "agent_started", AgentName: "codex", Payload: `{"model":"codex"}`, Timestamp: now.Add(time.Second)},
		{DebateID: debateID, EventType: "nudge_sent", AgentName: "claude", Payload: "", Timestamp: now.Add(2 * time.Second)},
	}

	for _, e := range events {
		if err := s.InsertEvent(e); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	got, err := s.GetDebateEvents(debateID)
	if err != nil {
		t.Fatalf("GetDebateEvents: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	// Verify ordering and auto-generated IDs.
	for i, e := range got {
		if e.ID == 0 {
			t.Errorf("event %d: ID should be auto-generated", i)
		}
	}
	if got[0].EventType != "agent_started" || got[0].AgentName != "claude" {
		t.Errorf("first event unexpected: %+v", got[0])
	}
}

func TestGetDebateEventsByType(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)
	now := time.Now().UTC()

	events := []Event{
		{DebateID: debateID, EventType: "agent_started", AgentName: "claude", Timestamp: now},
		{DebateID: debateID, EventType: "nudge_sent", AgentName: "claude", Timestamp: now.Add(time.Second)},
		{DebateID: debateID, EventType: "agent_started", AgentName: "codex", Timestamp: now.Add(2 * time.Second)},
		{DebateID: debateID, EventType: "error", AgentName: "codex", Payload: `{"msg":"timeout"}`, Timestamp: now.Add(3 * time.Second)},
	}

	for _, e := range events {
		if err := s.InsertEvent(e); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	got, err := s.GetDebateEventsByType(debateID, "agent_started")
	if err != nil {
		t.Fatalf("GetDebateEventsByType: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	got, err = s.GetDebateEventsByType(debateID, "error")
	if err != nil {
		t.Fatalf("GetDebateEventsByType: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Payload != `{"msg":"timeout"}` {
		t.Errorf("Payload = %q, want %q", got[0].Payload, `{"msg":"timeout"}`)
	}
}

// --- Concurrency ---

func TestConcurrentMessageInserts(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	const numGoroutines = 10
	const msgsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(goroutineIdx int) {
			defer wg.Done()
			for i := 0; i < msgsPerGoroutine; i++ {
				m := Message{
					ID:        uuid.New().String(),
					DebateID:  debateID,
					Author:    "agent",
					Content:   "concurrent message",
					Timestamp: time.Now().UTC(),
					TurnNum:   goroutineIdx*msgsPerGoroutine + i,
				}
				if err := s.InsertMessage(m); err != nil {
					t.Errorf("InsertMessage goroutine %d msg %d: %v", goroutineIdx, i, err)
				}
			}
		}(g)
	}

	wg.Wait()

	count, err := s.GetMessageCount(debateID)
	if err != nil {
		t.Fatalf("GetMessageCount: %v", err)
	}
	expected := numGoroutines * msgsPerGoroutine
	if count != expected {
		t.Errorf("count = %d, want %d", count, expected)
	}
}

// insertTestDebate is a helper that creates a debate and returns its ID.
func insertTestDebate(t *testing.T, s *Store) string {
	t.Helper()
	id := uuid.New().String()
	d := Debate{
		ID:        id,
		Question:  "test debate",
		Config:    "{}",
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.InsertDebate(d); err != nil {
		t.Fatalf("insertTestDebate: %v", err)
	}
	return id
}
