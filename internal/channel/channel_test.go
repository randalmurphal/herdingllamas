package channel

import (
	"sync"
	"testing"
	"time"

	"github.com/randalmurphal/herdingllamas/internal/store"
)

func mustOpenStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenInMemory()
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustCreateDebate(t *testing.T, s *store.Store) string {
	t.Helper()
	id := "debate-test-" + time.Now().Format("150405.000")
	err := s.InsertDebate(store.Debate{
		ID:        id,
		Question:  "test question",
		Config:    "{}",
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("InsertDebate: %v", err)
	}
	return id
}

func TestPostPersistsAndIncrementsTurn(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	msg1, err := ch.Post("claude", "First message")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if msg1.TurnNum != 0 {
		t.Errorf("TurnNum = %d, want 0", msg1.TurnNum)
	}
	if msg1.Author != "claude" {
		t.Errorf("Author = %q, want %q", msg1.Author, "claude")
	}
	if msg1.ID == "" {
		t.Error("ID should not be empty")
	}

	msg2, err := ch.Post("codex", "Second message")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if msg2.TurnNum != 1 {
		t.Errorf("TurnNum = %d, want 1", msg2.TurnNum)
	}

	// Verify persisted to store.
	count, err := st.GetMessageCount(debateID)
	if err != nil {
		t.Fatalf("GetMessageCount: %v", err)
	}
	if count != 2 {
		t.Errorf("store message count = %d, want 2", count)
	}
}

func TestSubscribersReceiveOtherAgentsMessages(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	claudeCh := ch.Subscribe("claude")
	codexCh := ch.Subscribe("codex")

	// Claude posts; codex should receive it.
	_, err := ch.Post("claude", "Hello from Claude")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	select {
	case msg := <-codexCh:
		if msg.Author != "claude" {
			t.Errorf("codex received msg from %q, want %q", msg.Author, "claude")
		}
		if msg.Content != "Hello from Claude" {
			t.Errorf("content = %q, want %q", msg.Content, "Hello from Claude")
		}
	case <-time.After(time.Second):
		t.Fatal("codex did not receive message within timeout")
	}

	// Claude should NOT receive their own message.
	select {
	case msg := <-claudeCh:
		t.Errorf("claude should not receive own message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected: no message for the author.
	}
}

func TestSubscribersDoNotReceiveOwnMessages(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	claudeCh := ch.Subscribe("claude")

	_, err := ch.Post("claude", "My own message")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	select {
	case msg := <-claudeCh:
		t.Errorf("should not receive own message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Good: no message received.
	}
}

func TestMultipleSubscribersGetSameMessage(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	sub1 := ch.Subscribe("agent1")
	sub2 := ch.Subscribe("agent2")
	sub3 := ch.Subscribe("agent3")

	_, err := ch.Post("system", "Broadcast message")
	if err != nil {
		t.Fatalf("Post: %v", err)
	}

	for _, sub := range []struct {
		name string
		ch   <-chan Message
	}{
		{"agent1", sub1},
		{"agent2", sub2},
		{"agent3", sub3},
	} {
		select {
		case msg := <-sub.ch:
			if msg.Content != "Broadcast message" {
				t.Errorf("%s received wrong content: %q", sub.name, msg.Content)
			}
		case <-time.After(time.Second):
			t.Errorf("%s did not receive message within timeout", sub.name)
		}
	}
}

func TestCursorTrackingAndPending(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	// Subscribe codex so its cursor starts at 0.
	ch.Subscribe("codex")
	// Manually set cursor to 0 to start tracking from the beginning.
	ch.MarkRead("codex", 0)

	// Post 3 messages.
	for i := 0; i < 3; i++ {
		if _, err := ch.Post("claude", "message"); err != nil {
			t.Fatalf("Post %d: %v", i, err)
		}
	}

	pending := ch.Pending("codex")
	if pending.UnreadCount != 3 {
		t.Errorf("UnreadCount = %d, want 3", pending.UnreadCount)
	}
}

func TestMarkReadAdvancesCursorReducesPending(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	ch.Subscribe("codex")
	ch.MarkRead("codex", 0)

	for i := 0; i < 5; i++ {
		if _, err := ch.Post("claude", "message"); err != nil {
			t.Fatalf("Post %d: %v", i, err)
		}
	}

	// Read up to turn 3 (exclusive of 3, so turns 0,1,2 are read).
	ch.MarkRead("codex", 3)

	pending := ch.Pending("codex")
	if pending.UnreadCount != 2 {
		t.Errorf("UnreadCount = %d, want 2", pending.UnreadCount)
	}

	// Read the rest.
	ch.MarkRead("codex", 5)
	pending = ch.Pending("codex")
	if pending.UnreadCount != 0 {
		t.Errorf("UnreadCount = %d, want 0", pending.UnreadCount)
	}
}

func TestMarkReadDoesNotGoBackward(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	ch.MarkRead("codex", 5)
	ch.MarkRead("codex", 2) // Should be ignored.

	ch.mu.RLock()
	cursor := ch.cursors["codex"]
	ch.mu.RUnlock()

	if cursor != 5 {
		t.Errorf("cursor = %d, want 5 (should not go backward)", cursor)
	}
}

func TestHistoryReturnsAllMessagesInOrder(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	authors := []string{"claude", "codex", "claude", "codex"}
	for _, a := range authors {
		if _, err := ch.Post(a, "message from "+a); err != nil {
			t.Fatalf("Post: %v", err)
		}
	}

	history, err := ch.History()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("len = %d, want 4", len(history))
	}

	for i, msg := range history {
		if msg.TurnNum != i {
			t.Errorf("message %d: TurnNum = %d, want %d", i, msg.TurnNum, i)
		}
		if msg.Author != authors[i] {
			t.Errorf("message %d: Author = %q, want %q", i, msg.Author, authors[i])
		}
	}
}

func TestLen(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	if ch.Len() != 0 {
		t.Errorf("Len = %d, want 0", ch.Len())
	}

	for i := 0; i < 3; i++ {
		if _, err := ch.Post("claude", "msg"); err != nil {
			t.Fatalf("Post: %v", err)
		}
	}

	if ch.Len() != 3 {
		t.Errorf("Len = %d, want 3", ch.Len())
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	sub := ch.Subscribe("claude")
	ch.Unsubscribe("claude")

	// Reading from a closed channel returns zero value immediately.
	_, ok := <-sub
	if ok {
		t.Error("expected channel to be closed after Unsubscribe")
	}
}

func TestUnsubscribeIdempotent(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	ch.Subscribe("claude")
	ch.Unsubscribe("claude")
	ch.Unsubscribe("claude") // Should not panic.
}

func TestSubscribeIdempotent(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	ch1 := ch.Subscribe("claude")
	ch2 := ch.Subscribe("claude")

	// Should return the same channel.
	if _, err := ch.Post("codex", "test"); err != nil {
		t.Fatalf("Post: %v", err)
	}

	// Both references should see the same message.
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatal("ch1 did not receive message")
	}

	// ch2 is the same channel, so it should be drained already.
	select {
	case <-ch2:
		t.Error("ch2 should not have a second message (same channel)")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestConcurrentPosts(t *testing.T) {
	st := mustOpenStore(t)
	debateID := mustCreateDebate(t, st)
	ch := New(debateID, st)

	const numGoroutines = 10
	const postsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(idx int) {
			defer wg.Done()
			for i := 0; i < postsPerGoroutine; i++ {
				_, err := ch.Post("agent", "concurrent message")
				if err != nil {
					t.Errorf("Post goroutine %d msg %d: %v", idx, i, err)
				}
			}
		}(g)
	}

	wg.Wait()

	expected := numGoroutines * postsPerGoroutine
	if ch.Len() != expected {
		t.Errorf("Len = %d, want %d", ch.Len(), expected)
	}

	// Verify all messages are in the store.
	history, err := ch.History()
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != expected {
		t.Errorf("history len = %d, want %d", len(history), expected)
	}
}
