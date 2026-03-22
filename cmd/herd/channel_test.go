package main

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/randalmurphal/herdingllamas/internal/store"
)

// mustOpenStore opens an in-memory store for testing.
func mustOpenStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenInMemory()
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// insertActiveDebate creates an active debate and returns its ID.
func insertActiveDebate(t *testing.T, s *store.Store) string {
	t.Helper()
	id := uuid.New().String()
	d := store.Debate{
		ID:        id,
		Question:  "test debate",
		Config:    "{}",
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.InsertDebate(d); err != nil {
		t.Fatalf("insertActiveDebate: %v", err)
	}
	return id
}

// --- PostMessage turn numbering ---

func TestPostMessageAssignsSequentialTurnNumbers(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

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

	msg2, err := s.PostMessage(debateID, "claude", "third message")
	if err != nil {
		t.Fatalf("PostMessage 2: %v", err)
	}
	if msg2.TurnNum != 2 {
		t.Errorf("third message TurnNum = %d, want 2", msg2.TurnNum)
	}
}

func TestPostMessageReturnsCorrectFields(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	msg, err := s.PostMessage(debateID, "claude", "hello world")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	if msg.ID == "" {
		t.Error("PostMessage returned empty ID")
	}
	if msg.DebateID != debateID {
		t.Errorf("DebateID = %q, want %q", msg.DebateID, debateID)
	}
	if msg.Author != "claude" {
		t.Errorf("Author = %q, want %q", msg.Author, "claude")
	}
	if msg.Content != "hello world" {
		t.Errorf("Content = %q, want %q", msg.Content, "hello world")
	}
	if msg.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestPostMessageToDifferentDebatesHaveIndependentTurns(t *testing.T) {
	s := mustOpenStore(t)
	debate1 := insertActiveDebate(t, s)
	debate2 := insertActiveDebate(t, s)

	msg1a, err := s.PostMessage(debate1, "claude", "debate1 first")
	if err != nil {
		t.Fatalf("PostMessage debate1: %v", err)
	}
	msg2a, err := s.PostMessage(debate2, "codex", "debate2 first")
	if err != nil {
		t.Fatalf("PostMessage debate2: %v", err)
	}

	// Both debates should start at turn 0 independently.
	if msg1a.TurnNum != 0 {
		t.Errorf("debate1 first TurnNum = %d, want 0", msg1a.TurnNum)
	}
	if msg2a.TurnNum != 0 {
		t.Errorf("debate2 first TurnNum = %d, want 0", msg2a.TurnNum)
	}

	// Second message in debate1 should be turn 1, unaffected by debate2.
	msg1b, err := s.PostMessage(debate1, "claude", "debate1 second")
	if err != nil {
		t.Fatalf("PostMessage debate1 second: %v", err)
	}
	if msg1b.TurnNum != 1 {
		t.Errorf("debate1 second TurnNum = %d, want 1", msg1b.TurnNum)
	}
}

// --- Cursor tracking (post workflow) ---

func TestUpdateCursorAdvancesPastOwnMessage(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	// Simulate the channelPostCmd workflow: post, then advance cursor.
	msg, err := s.PostMessage(debateID, "claude", "my message")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if err := s.UpdateCursor(debateID, "claude", msg.TurnNum); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	// Claude's own message should not appear as unread for claude.
	unread, err := s.GetUnreadMessages(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("expected 0 unread for claude after posting, got %d", len(unread))
	}
}

func TestCursorOnlyAdvancesForward(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	// Post two messages.
	s.PostMessage(debateID, "claude", "turn 0")
	msg1, _ := s.PostMessage(debateID, "codex", "turn 1")

	// Advance cursor to turn 1.
	if err := s.UpdateCursor(debateID, "claude", msg1.TurnNum); err != nil {
		t.Fatalf("UpdateCursor to 1: %v", err)
	}

	// Attempt to move cursor backward to turn 0.
	if err := s.UpdateCursor(debateID, "claude", 0); err != nil {
		t.Fatalf("UpdateCursor to 0: %v", err)
	}

	// Post a third message; only it should be unread for claude.
	s.PostMessage(debateID, "codex", "turn 2")

	unread, err := s.GetUnreadMessages(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(unread) != 1 {
		t.Fatalf("expected 1 unread after backward cursor attempt, got %d", len(unread))
	}
	if unread[0].TurnNum != 2 {
		t.Errorf("unread message TurnNum = %d, want 2", unread[0].TurnNum)
	}
}

// --- GetUnreadMessages (read workflow) ---

func TestGetUnreadMessagesReturnsAllWhenNoCursor(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	s.PostMessage(debateID, "claude", "msg 0")
	s.PostMessage(debateID, "codex", "msg 1")
	s.PostMessage(debateID, "claude", "msg 2")

	// Codex has never read - should get all messages (including own).
	unread, err := s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(unread) != 3 {
		t.Errorf("expected 3 unread with no cursor, got %d", len(unread))
	}
}

func TestGetUnreadMessagesReturnsSinceCursor(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	s.PostMessage(debateID, "claude", "msg 0")
	s.PostMessage(debateID, "codex", "msg 1")

	// Advance codex cursor to turn 1 (simulates having read through turn 1).
	if err := s.UpdateCursor(debateID, "codex", 1); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	// Post more messages.
	s.PostMessage(debateID, "claude", "msg 2")
	s.PostMessage(debateID, "codex", "msg 3")

	unread, err := s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(unread) != 2 {
		t.Fatalf("expected 2 unread since cursor at 1, got %d", len(unread))
	}
	if unread[0].TurnNum != 2 {
		t.Errorf("first unread TurnNum = %d, want 2", unread[0].TurnNum)
	}
	if unread[1].TurnNum != 3 {
		t.Errorf("second unread TurnNum = %d, want 3", unread[1].TurnNum)
	}
}

func TestGetUnreadMessagesReturnsEmptyWhenCaughtUp(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	msg, _ := s.PostMessage(debateID, "claude", "only message")

	// Advance cursor past the only message.
	if err := s.UpdateCursor(debateID, "codex", msg.TurnNum); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	unread, err := s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("expected 0 unread when caught up, got %d", len(unread))
	}
}

func TestGetUnreadMessagesEmptyDebate(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	unread, err := s.GetUnreadMessages(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(unread) != 0 {
		t.Errorf("expected 0 unread in empty debate, got %d", len(unread))
	}
}

// --- Read workflow: cursor advances after read ---

func TestReadWorkflowAdvancesCursorToLastMessage(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	s.PostMessage(debateID, "claude", "msg 0")
	s.PostMessage(debateID, "codex", "msg 1")
	s.PostMessage(debateID, "claude", "msg 2")

	// Simulate the channelReadCmd workflow for codex:
	// 1. Read unread messages.
	messages, err := s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// 2. Advance cursor to the last message's turn.
	lastTurn := messages[len(messages)-1].TurnNum
	if err := s.UpdateCursor(debateID, "codex", lastTurn); err != nil {
		t.Fatalf("UpdateCursor: %v", err)
	}

	// 3. Subsequent read should return nothing.
	messages, err = s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages after cursor advance: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("expected 0 unread after cursor advance, got %d", len(messages))
	}

	// 4. New message should appear as unread.
	s.PostMessage(debateID, "claude", "msg 3")
	messages, err = s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages after new post: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 unread after new post, got %d", len(messages))
	}
	if messages[0].Content != "msg 3" {
		t.Errorf("Content = %q, want %q", messages[0].Content, "msg 3")
	}
}

// --- Post workflow: full round-trip ---

func TestPostWorkflowDoesNotShowOwnMessagesAsUnread(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	// Simulate two agents alternating posts with the channelPostCmd pattern.
	// Claude posts and advances its cursor.
	msg0, _ := s.PostMessage(debateID, "claude", "claude turn 0")
	s.UpdateCursor(debateID, "claude", msg0.TurnNum)

	// Codex posts and advances its cursor.
	msg1, _ := s.PostMessage(debateID, "codex", "codex turn 1")
	s.UpdateCursor(debateID, "codex", msg1.TurnNum)

	// Claude should see codex's message but not its own.
	unreadClaude, err := s.GetUnreadMessages(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadMessages claude: %v", err)
	}
	if len(unreadClaude) != 1 {
		t.Fatalf("expected 1 unread for claude, got %d", len(unreadClaude))
	}
	if unreadClaude[0].Author != "codex" {
		t.Errorf("expected unread from codex, got author %q", unreadClaude[0].Author)
	}

	// Codex has already advanced past its own message, so it has no unread.
	unreadCodex, err := s.GetUnreadMessages(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadMessages codex: %v", err)
	}
	if len(unreadCodex) != 0 {
		t.Errorf("expected 0 unread for codex (cursor at own msg), got %d", len(unreadCodex))
	}
}

// --- GetUnreadCount (used by nudge loop, complements read workflow) ---

func TestGetUnreadCountExcludesSelfMessages(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	// Claude posts three messages in a row.
	for i := 0; i < 3; i++ {
		s.PostMessage(debateID, "claude", "claude message")
	}

	// Unread count for claude should be 0 (GetUnreadCount excludes self).
	count, err := s.GetUnreadCount(debateID, "claude")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 unread count for self messages, got %d", count)
	}

	// Unread count for codex should be 3 (all from others, no cursor yet).
	count, err = s.GetUnreadCount(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 unread count for codex, got %d", count)
	}
}

func TestGetUnreadCountRespectsAgentCursor(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	s.PostMessage(debateID, "claude", "turn 0")
	s.PostMessage(debateID, "claude", "turn 1")

	// Advance codex cursor to turn 0.
	s.UpdateCursor(debateID, "codex", 0)

	// Only turn 1 should count as unread for codex.
	count, err := s.GetUnreadCount(debateID, "codex")
	if err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 unread count after cursor advance, got %d", count)
	}
}

// --- Edge case: debate lookup ---

func TestGetDebateNonexistentReturnsNil(t *testing.T) {
	s := mustOpenStore(t)

	debate, err := s.GetDebate("does-not-exist")
	if err != nil {
		t.Fatalf("GetDebate: %v", err)
	}
	if debate != nil {
		t.Errorf("expected nil for nonexistent debate, got %+v", debate)
	}
}

func TestPostWorkflowAgainstNonActiveDebate(t *testing.T) {
	s := mustOpenStore(t)
	id := uuid.New().String()
	d := store.Debate{
		ID:        id,
		Question:  "completed debate",
		Config:    "{}",
		Status:    "completed",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.InsertDebate(d); err != nil {
		t.Fatalf("InsertDebate: %v", err)
	}

	// The channelPostCmd checks status == "active" before posting.
	// Verify the debate status so the command would reject it.
	debate, err := s.GetDebate(id)
	if err != nil {
		t.Fatalf("GetDebate: %v", err)
	}
	if debate.Status == "active" {
		t.Fatal("debate should not be active")
	}

	// PostMessage itself doesn't enforce status (that's the command's job),
	// but the command would check GetDebate and reject non-active debates.
	// Verify the check the command performs:
	if debate.Status != "completed" {
		t.Errorf("Status = %q, want %q", debate.Status, "completed")
	}
}

// --- Ordering ---

// --- unescapeContent ---

func TestUnescapeContentNewlines(t *testing.T) {
	got := unescapeContent(`Here is my analysis:\n\n1. First point\n2. Second point`)
	want := "Here is my analysis:\n\n1. First point\n2. Second point"
	if got != want {
		t.Errorf("unescapeContent newlines:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestUnescapeContentTabs(t *testing.T) {
	got := unescapeContent(`column1\tcolumn2`)
	want := "column1\tcolumn2"
	if got != want {
		t.Errorf("unescapeContent tabs:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestUnescapeContentNoEscapes(t *testing.T) {
	input := "plain text with no escapes"
	got := unescapeContent(input)
	if got != input {
		t.Errorf("unescapeContent plain:\ngot:  %q\nwant: %q", got, input)
	}
}

func TestUnescapeContentPreservesRealNewlines(t *testing.T) {
	input := "line1\nline2"
	got := unescapeContent(input)
	if got != input {
		t.Errorf("unescapeContent real newlines:\ngot:  %q\nwant: %q", got, input)
	}
}

// --- Ordering ---

func TestGetUnreadMessagesOrderedByTurnNum(t *testing.T) {
	s := mustOpenStore(t)
	debateID := insertActiveDebate(t, s)

	// Post several messages from different agents.
	s.PostMessage(debateID, "claude", "first")
	s.PostMessage(debateID, "codex", "second")
	s.PostMessage(debateID, "claude", "third")
	s.PostMessage(debateID, "system", "fourth")

	unread, err := s.GetUnreadMessages(debateID, "observer")
	if err != nil {
		t.Fatalf("GetUnreadMessages: %v", err)
	}
	if len(unread) != 4 {
		t.Fatalf("expected 4 unread, got %d", len(unread))
	}

	for i := 0; i < len(unread)-1; i++ {
		if unread[i].TurnNum >= unread[i+1].TurnNum {
			t.Errorf("messages not ordered: turn %d at index %d, turn %d at index %d",
				unread[i].TurnNum, i, unread[i+1].TurnNum, i+1)
		}
	}
}
