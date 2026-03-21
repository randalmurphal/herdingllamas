package store

import (
	"fmt"
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

// --- PostMessage ---

func TestPostMessageAssignsTurnNumbers(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	msg0, err := s.PostMessage(debateID, "claude", "first message")
	if err != nil {
		t.Fatalf("PostMessage 0: %v", err)
	}
	if msg0.TurnNum != 0 {
		t.Errorf("first message TurnNum = %d, want 0", msg0.TurnNum)
	}

	msg1, err := s.PostMessage(debateID, "codex", "second message")
	if err != nil {
		t.Fatalf("PostMessage 1: %v", err)
	}
	if msg1.TurnNum != 1 {
		t.Errorf("second message TurnNum = %d, want 1", msg1.TurnNum)
	}
}

func TestPostMessagePersistsMessage(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	posted, err := s.PostMessage(debateID, "claude", "persisted content")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	msgs, err := s.GetDebateMessages(debateID)
	if err != nil {
		t.Fatalf("GetDebateMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].ID != posted.ID {
		t.Errorf("ID = %q, want %q", msgs[0].ID, posted.ID)
	}
	if msgs[0].Author != "claude" {
		t.Errorf("Author = %q, want %q", msgs[0].Author, "claude")
	}
	if msgs[0].Content != "persisted content" {
		t.Errorf("Content = %q, want %q", msgs[0].Content, "persisted content")
	}
	if msgs[0].DebateID != debateID {
		t.Errorf("DebateID = %q, want %q", msgs[0].DebateID, debateID)
	}
}

func TestPostMessageConcurrentNoDuplicateTurns(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := s.PostMessage(debateID, "agent", "concurrent"); err != nil {
				t.Errorf("PostMessage: %v", err)
			}
		}()
	}
	wg.Wait()

	msgs, err := s.GetDebateMessages(debateID)
	if err != nil {
		t.Fatalf("GetDebateMessages: %v", err)
	}
	if len(msgs) != numGoroutines {
		t.Fatalf("len = %d, want %d", len(msgs), numGoroutines)
	}

	// Verify every turn number from 0..numGoroutines-1 appears exactly once.
	seen := make(map[int]bool)
	for _, m := range msgs {
		if seen[m.TurnNum] {
			t.Errorf("duplicate turn number: %d", m.TurnNum)
		}
		seen[m.TurnNum] = true
	}
	for i := 0; i < numGoroutines; i++ {
		if !seen[i] {
			t.Errorf("missing turn number: %d", i)
		}
	}
}

// --- GetUnreadCount ---

func TestGetUnreadCountNoCursor(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Post messages from two agents.
	if _, err := s.PostMessage(debateID, "claude", "msg 0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "msg 1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "msg 2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// codex has no cursor — all non-codex messages should be unread.
	count, err := s.GetUnreadCount(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 2 {
		t.Errorf("unread count = %d, want 2 (two claude messages)", count)
	}

	// claude has no cursor — all non-claude messages should be unread.
	count, err = s.GetUnreadCount(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 1 {
		t.Errorf("unread count = %d, want 1 (one codex message)", count)
	}
}

func TestGetUnreadCountAfterCursorUpdate(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Post 4 messages: claude, codex, claude, codex.
	if _, err := s.PostMessage(debateID, "claude", "t0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "t1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "t2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "t3"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// codex reads through turn 2 — only turn 3 (codex's own) remains, but
	// GetUnreadCount excludes self, so 0 unread from others.
	if err := s.UpdateCursor(debateID, "codex", 2); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}
	count, err := s.GetUnreadCount(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 0 {
		t.Errorf("unread count = %d, want 0", count)
	}

	// claude reads through turn 1 — turns 2 (claude's own) and 3 (codex) remain.
	// Only codex's message at turn 3 counts (self excluded).
	if err := s.UpdateCursor(debateID, "claude", 1); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}
	count, err = s.GetUnreadCount(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 1 {
		t.Errorf("unread count = %d, want 1", count)
	}
}

func TestGetUnreadCountExcludesSelf(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Only self-messages — unread count should be 0.
	if _, err := s.PostMessage(debateID, "claude", "my msg 0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "my msg 1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	count, err := s.GetUnreadCount(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 0 {
		t.Errorf("unread count = %d, want 0 (all messages are self)", count)
	}
}

// --- GetUnreadMessages ---

func TestGetUnreadMessagesNoCursor(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	if _, err := s.PostMessage(debateID, "claude", "t0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "t1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// No cursor — should return all messages.
	msgs, err := s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2", len(msgs))
	}
}

func TestGetUnreadMessagesAfterCursorUpdate(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	if _, err := s.PostMessage(debateID, "claude", "t0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "t1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "t2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// codex reads through turn 1 — only turn 2 should be returned.
	if err := s.UpdateCursor(debateID, "codex", 1); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	msgs, err := s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d, want 1", len(msgs))
	}
	if msgs[0].TurnNum != 2 {
		t.Errorf("TurnNum = %d, want 2", msgs[0].TurnNum)
	}
}

func TestGetUnreadMessagesIncludesOwnMessages(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	if _, err := s.PostMessage(debateID, "claude", "t0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "t1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "t2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// claude reads through turn 0 — turns 1 and 2 remain.
	// GetUnreadMessages includes own messages (turn 2 is claude's).
	if err := s.UpdateCursor(debateID, "claude", 0); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	msgs, err := s.GetUnreadMessages(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len = %d, want 2 (includes own message at turn 2)", len(msgs))
	}

	// Verify both are present: codex at turn 1, claude at turn 2.
	if msgs[0].Author != "codex" || msgs[0].TurnNum != 1 {
		t.Errorf("msgs[0] = author=%q turn=%d, want codex/1", msgs[0].Author, msgs[0].TurnNum)
	}
	if msgs[1].Author != "claude" || msgs[1].TurnNum != 2 {
		t.Errorf("msgs[1] = author=%q turn=%d, want claude/2", msgs[1].Author, msgs[1].TurnNum)
	}
}

func TestGetUnreadMessagesOrderedByTurnNum(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Post 5 messages.
	for i := 0; i < 5; i++ {
		author := "claude"
		if i%2 == 1 {
			author = "codex"
		}
		if _, err := s.PostMessage(debateID, author, fmt.Sprintf("t%d", i)); err != nil {
			t.Fatalf("PostMessage %d: %v", i, err)
		}
	}

	msgs, err := s.GetUnreadMessages(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("len = %d, want 5", len(msgs))
	}

	for i, m := range msgs {
		if m.TurnNum != i {
			t.Errorf("msgs[%d].TurnNum = %d, want %d", i, m.TurnNum, i)
		}
	}
}

// --- UpdateCursor ---

func TestUpdateCursorCreatesOnFirstCall(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Post a message so there's something to read.
	if _, err := s.PostMessage(debateID, "claude", "t0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "t1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Before cursor: 2 messages unread for codex.
	count, err := s.GetUnreadCount(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 2 {
		t.Errorf("before cursor: unread = %d, want 2", count)
	}

	// Create cursor at turn 1 — should mark everything through turn 1 as read.
	if err := s.UpdateCursor(debateID, "codex", 1); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	count, err = s.GetUnreadCount(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 0 {
		t.Errorf("after cursor at 1: unread = %d, want 0", count)
	}
}

func TestUpdateCursorAdvancesForward(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	for i := 0; i < 4; i++ {
		if _, err := s.PostMessage(debateID, "codex", fmt.Sprintf("t%d", i)); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}

	// Set cursor to turn 1, then advance to turn 3.
	if err := s.UpdateCursor(debateID, "claude", 1); err != nil {
		t.Fatalf("UpdateCursor to 1: %v", err)
	}
	if err := s.UpdateCursor(debateID, "claude", 3); err != nil {
		t.Fatalf("UpdateCursor to 3: %v", err)
	}

	// All 4 messages are from codex, claude has cursor at 3 — 0 unread.
	count, err := s.GetUnreadCount(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 0 {
		t.Errorf("unread = %d, want 0", count)
	}
}

func TestUpdateCursorDoesNotMoveBackward(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	for i := 0; i < 5; i++ {
		if _, err := s.PostMessage(debateID, "codex", fmt.Sprintf("t%d", i)); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}

	// Set cursor to turn 3, then try to move it back to turn 1.
	if err := s.UpdateCursor(debateID, "claude", 3); err != nil {
		t.Fatalf("UpdateCursor to 3: %v", err)
	}
	if err := s.UpdateCursor(debateID, "claude", 1); err != nil {
		t.Fatalf("UpdateCursor to 1: %v", err)
	}

	// Cursor should still be at 3. Only turn 4 (from codex) is unread.
	count, err := s.GetUnreadCount(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 1 {
		t.Errorf("unread = %d, want 1 (cursor should not have moved backward)", count)
	}

	msgs, err := s.GetUnreadMessages(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("unread msgs len = %d, want 1", len(msgs))
	}
	if msgs[0].TurnNum != 4 {
		t.Errorf("unread msg TurnNum = %d, want 4", msgs[0].TurnNum)
	}
}

// --- Conclusions ---

func TestSetConcluded(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	if err := s.SetConcluded(debateID, "claude"); err != nil {
		t.Fatalf("SetConcluded: %v", err)
	}

	concluded, err := s.GetConcluded(debateID)
	if err != nil {
		t.Fatalf("GetConcluded: %v", err)
	}
	if len(concluded) != 1 {
		t.Fatalf("len = %d, want 1", len(concluded))
	}
	if concluded[0] != "claude" {
		t.Errorf("concluded[0] = %q, want %q", concluded[0], "claude")
	}
}

func TestSetConcludedIdempotent(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Calling SetConcluded twice should not error or create duplicates.
	if err := s.SetConcluded(debateID, "claude"); err != nil {
		t.Fatalf("SetConcluded first: %v", err)
	}
	if err := s.SetConcluded(debateID, "claude"); err != nil {
		t.Fatalf("SetConcluded second: %v", err)
	}

	concluded, err := s.GetConcluded(debateID)
	if err != nil {
		t.Fatalf("GetConcluded: %v", err)
	}
	if len(concluded) != 1 {
		t.Fatalf("len = %d, want 1 (idempotent upsert)", len(concluded))
	}
}

func TestRevokeConcluded(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	if err := s.SetConcluded(debateID, "claude"); err != nil {
		t.Fatalf("SetConcluded: %v", err)
	}

	if err := s.RevokeConcluded(debateID, "claude"); err != nil {
		t.Fatalf("RevokeConcluded: %v", err)
	}

	concluded, err := s.GetConcluded(debateID)
	if err != nil {
		t.Fatalf("GetConcluded: %v", err)
	}
	if len(concluded) != 0 {
		t.Errorf("len = %d, want 0 after revoke", len(concluded))
	}
}

func TestRevokeConcludedNoOp(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Revoking when no conclusion exists should not error.
	if err := s.RevokeConcluded(debateID, "claude"); err != nil {
		t.Fatalf("RevokeConcluded (no-op): %v", err)
	}
}

func TestGetConcludedEmpty(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	concluded, err := s.GetConcluded(debateID)
	if err != nil {
		t.Fatalf("GetConcluded: %v", err)
	}
	if len(concluded) != 0 {
		t.Errorf("len = %d, want 0", len(concluded))
	}
}

func TestGetConcludedMultipleAgents(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	if err := s.SetConcluded(debateID, "claude"); err != nil {
		t.Fatalf("SetConcluded claude: %v", err)
	}
	if err := s.SetConcluded(debateID, "codex"); err != nil {
		t.Fatalf("SetConcluded codex: %v", err)
	}

	concluded, err := s.GetConcluded(debateID)
	if err != nil {
		t.Fatalf("GetConcluded: %v", err)
	}
	if len(concluded) != 2 {
		t.Fatalf("len = %d, want 2", len(concluded))
	}
}

func TestAllConcluded(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// No conclusions yet.
	all, err := s.AllConcluded(debateID, 2)
	if err != nil {
		t.Fatalf("AllConcluded: %v", err)
	}
	if all {
		t.Error("AllConcluded should be false with 0 conclusions and 2 expected")
	}

	// One conclusion.
	if err := s.SetConcluded(debateID, "claude"); err != nil {
		t.Fatalf("SetConcluded: %v", err)
	}
	all, err = s.AllConcluded(debateID, 2)
	if err != nil {
		t.Fatalf("AllConcluded: %v", err)
	}
	if all {
		t.Error("AllConcluded should be false with 1 conclusion and 2 expected")
	}

	// Both concluded.
	if err := s.SetConcluded(debateID, "codex"); err != nil {
		t.Fatalf("SetConcluded: %v", err)
	}
	all, err = s.AllConcluded(debateID, 2)
	if err != nil {
		t.Fatalf("AllConcluded: %v", err)
	}
	if !all {
		t.Error("AllConcluded should be true with 2 conclusions and 2 expected")
	}
}

func TestConcludeRevokedByPost(t *testing.T) {
	// Simulates the full workflow: conclude, then post revokes conclusion.
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Agent concludes.
	if err := s.SetConcluded(debateID, "claude"); err != nil {
		t.Fatalf("SetConcluded: %v", err)
	}

	concluded, err := s.GetConcluded(debateID)
	if err != nil {
		t.Fatalf("GetConcluded: %v", err)
	}
	if len(concluded) != 1 {
		t.Fatalf("len = %d, want 1 after conclude", len(concluded))
	}

	// Agent posts a new message — revokes conclusion.
	if _, err := s.PostMessage(debateID, "claude", "actually, one more point..."); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if err := s.RevokeConcluded(debateID, "claude"); err != nil {
		t.Fatalf("RevokeConcluded: %v", err)
	}

	concluded, err = s.GetConcluded(debateID)
	if err != nil {
		t.Fatalf("GetConcluded after post: %v", err)
	}
	if len(concluded) != 0 {
		t.Errorf("len = %d, want 0 after post revokes conclusion", len(concluded))
	}

	// AllConcluded should be false.
	all, err := s.AllConcluded(debateID, 2)
	if err != nil {
		t.Fatalf("AllConcluded: %v", err)
	}
	if all {
		t.Error("AllConcluded should be false after revoke")
	}
}

func TestConclusionsIndependentPerDebate(t *testing.T) {
	s := mustOpen(t)
	debateID1 := insertTestDebate(t, s)
	debateID2 := insertTestDebate(t, s)

	if err := s.SetConcluded(debateID1, "claude"); err != nil {
		t.Fatalf("SetConcluded debate1: %v", err)
	}

	concluded1, err := s.GetConcluded(debateID1)
	if err != nil {
		t.Fatalf("GetConcluded debate1: %v", err)
	}
	concluded2, err := s.GetConcluded(debateID2)
	if err != nil {
		t.Fatalf("GetConcluded debate2: %v", err)
	}

	if len(concluded1) != 1 {
		t.Errorf("debate1 conclusions = %d, want 1", len(concluded1))
	}
	if len(concluded2) != 0 {
		t.Errorf("debate2 conclusions = %d, want 0", len(concluded2))
	}
}

// --- GetAgentStatuses ---

func TestGetAgentStatuses(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Empty debate — no statuses.
	statuses, err := s.GetAgentStatuses(debateID)
	if err != nil {
		t.Fatalf("GetAgentStatuses empty: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses for empty debate, got %d", len(statuses))
	}

	// Post messages from two agents.
	if _, err := s.PostMessage(debateID, "claude", "t0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "t1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "t2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Also add a moderator message — should not appear in statuses.
	if _, err := s.PostMessage(debateID, "moderator", "opening"); err != nil {
		t.Fatalf("PostMessage moderator: %v", err)
	}

	// Set claude's cursor to turn 1.
	if err := s.UpdateCursor(debateID, "claude", 1); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	statuses, err = s.GetAgentStatuses(debateID)
	if err != nil {
		t.Fatalf("GetAgentStatuses: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	// Results are ordered by name ASC.
	claude := statuses[0]
	codex := statuses[1]

	if claude.Name != "claude" {
		t.Errorf("statuses[0].Name = %q, want %q", claude.Name, "claude")
	}
	if claude.LastReadTurn != 1 {
		t.Errorf("claude.LastReadTurn = %d, want 1", claude.LastReadTurn)
	}
	if claude.LastPostTurn != 2 {
		t.Errorf("claude.LastPostTurn = %d, want 2", claude.LastPostTurn)
	}

	if codex.Name != "codex" {
		t.Errorf("statuses[1].Name = %q, want %q", codex.Name, "codex")
	}
	if codex.LastReadTurn != -1 {
		t.Errorf("codex.LastReadTurn = %d, want -1 (no cursor)", codex.LastReadTurn)
	}
	if codex.LastPostTurn != 1 {
		t.Errorf("codex.LastPostTurn = %d, want 1", codex.LastPostTurn)
	}
}

// --- GetLatestTurnNum ---

func TestGetLatestTurnNum(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Empty debate should return -1.
	turn, err := s.GetLatestTurnNum(debateID)
	if err != nil {
		t.Fatalf("GetLatestTurnNum empty: %v", err)
	}
	if turn != -1 {
		t.Errorf("empty debate turn = %d, want -1", turn)
	}

	// Post some messages.
	if _, err := s.PostMessage(debateID, "claude", "t0"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "t1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "t2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	turn, err = s.GetLatestTurnNum(debateID)
	if err != nil {
		t.Fatalf("GetLatestTurnNum: %v", err)
	}
	if turn != 2 {
		t.Errorf("latest turn = %d, want 2", turn)
	}
}

// --- GetMessagesAfterTurn ---

func TestGetMessagesAfterTurn(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Post messages: moderator at 0, claude at 1, codex at 2, claude at 3.
	if _, err := s.PostMessage(debateID, "moderator", "opening"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "t1"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "codex", "t2"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if _, err := s.PostMessage(debateID, "claude", "t3"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	// Get messages after turn 1, excluding claude — should only get codex at turn 2.
	msgs, err := s.GetMessagesAfterTurn(debateID, 1, "claude")
	if err != nil {
		t.Fatalf("GetMessagesAfterTurn: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Author != "codex" || msgs[0].TurnNum != 2 {
		t.Errorf("got author=%q turn=%d, want codex/2", msgs[0].Author, msgs[0].TurnNum)
	}

	// Get messages after turn 0, excluding codex — should get claude at turns 1 and 3
	// (moderator is excluded by the query).
	msgs, err = s.GetMessagesAfterTurn(debateID, 0, "codex")
	if err != nil {
		t.Fatalf("GetMessagesAfterTurn: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].TurnNum != 1 || msgs[1].TurnNum != 3 {
		t.Errorf("got turns %d, %d; want 1, 3", msgs[0].TurnNum, msgs[1].TurnNum)
	}

	// Get messages after the latest turn — should be empty.
	msgs, err = s.GetMessagesAfterTurn(debateID, 3, "codex")
	if err != nil {
		t.Fatalf("GetMessagesAfterTurn: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after latest turn, got %d", len(msgs))
	}
}

// --- HasCursorAdvancedPast ---

func TestHasCursorAdvancedPast(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// No cursors at all — should return false.
	_, advanced, err := s.HasCursorAdvancedPast(debateID, "claude", 0)
	if err != nil {
		t.Fatalf("HasCursorAdvancedPast: %v", err)
	}
	if advanced {
		t.Error("expected false with no cursors")
	}

	// Set codex cursor to turn 2.
	if err := s.UpdateCursor(debateID, "codex", 2); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	// Check if anyone other than claude has read past turn 1 — codex at 2 qualifies.
	name, advanced, err := s.HasCursorAdvancedPast(debateID, "claude", 1)
	if err != nil {
		t.Fatalf("HasCursorAdvancedPast: %v", err)
	}
	if !advanced {
		t.Error("expected true, codex cursor is at 2 which is >= 1")
	}
	if name != "codex" {
		t.Errorf("agent name = %q, want %q", name, "codex")
	}

	// Check if anyone other than claude has read past turn 3 — codex at 2 doesn't qualify.
	_, advanced, err = s.HasCursorAdvancedPast(debateID, "claude", 3)
	if err != nil {
		t.Fatalf("HasCursorAdvancedPast: %v", err)
	}
	if advanced {
		t.Error("expected false, codex cursor is at 2 which is < 3")
	}

	// Check that it excludes the asking agent's own cursor.
	if err := s.UpdateCursor(debateID, "claude", 5); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}
	_, advanced, err = s.HasCursorAdvancedPast(debateID, "claude", 3)
	if err != nil {
		t.Fatalf("HasCursorAdvancedPast: %v", err)
	}
	if advanced {
		t.Error("expected false, should exclude claude's own cursor")
	}
}

func TestUpdateCursorIndependentPerAgent(t *testing.T) {
	s := mustOpen(t)
	debateID := insertTestDebate(t, s)

	// Post 3 messages from a third party so both agents have unread.
	for i := 0; i < 3; i++ {
		if _, err := s.PostMessage(debateID, "gemini", fmt.Sprintf("t%d", i)); err != nil {
			t.Fatalf("PostMessage: %v", err)
		}
	}

	// claude reads through turn 2, codex reads through turn 0.
	if err := s.UpdateCursor(debateID, "claude", 2); err != nil {
		t.Fatalf("UpdateCursor claude: %v", err)
	}
	if err := s.UpdateCursor(debateID, "codex", 0); err != nil {
		t.Fatalf("UpdateCursor codex: %v", err)
	}

	claudeUnread, err := s.GetUnreadCount(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadCount claude: %v", err)
	}
	codexUnread, err := s.GetUnreadCount(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadCount codex: %v", err)
	}

	if claudeUnread != 0 {
		t.Errorf("claude unread = %d, want 0", claudeUnread)
	}
	if codexUnread != 2 {
		t.Errorf("codex unread = %d, want 2", codexUnread)
	}
}
