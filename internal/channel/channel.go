package channel

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/randalmurphal/herdingllamas/internal/store"
)

// subscriberBufferSize is the capacity of each subscriber's message channel.
const subscriberBufferSize = 50

// Channel provides message persistence and pub/sub for debate agents.
// Messages are persisted to SQLite via the store and delivered to
// subscribers via Go channels. Each agent subscribes by name and receives
// messages posted by other agents (never their own).
type Channel struct {
	mu          sync.RWMutex
	debateID    string
	store       *store.Store
	subscribers map[string]chan Message // agent name -> message channel
	cursors     map[string]int         // agent name -> last read turn_num
	nextTurn    int                    // next turn number to assign
}

// New creates a Channel for the given debate, backed by the provided store.
func New(debateID string, st *store.Store) *Channel {
	return &Channel{
		debateID:    debateID,
		store:       st,
		subscribers: make(map[string]chan Message),
		cursors:     make(map[string]int),
		nextTurn:    0,
	}
}

// Post adds a message from an agent. The message is persisted to SQLite first,
// then delivered to all other subscribers (the author does not receive their
// own message).
func (c *Channel) Post(author, content string) (Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UTC()
	turn := c.nextTurn
	c.nextTurn++

	msg := Message{
		ID:        uuid.New().String(),
		Author:    author,
		Content:   content,
		Timestamp: now,
		TurnNum:   turn,
	}

	// Persist to store first so the database is always authoritative.
	err := c.store.InsertMessage(store.Message{
		ID:        msg.ID,
		DebateID:  c.debateID,
		Author:    msg.Author,
		Content:   msg.Content,
		Timestamp: msg.Timestamp,
		TurnNum:   msg.TurnNum,
	})
	if err != nil {
		// Roll back the turn counter since the message wasn't persisted.
		c.nextTurn--
		return Message{}, fmt.Errorf("persisting message: %w", err)
	}

	// Deliver to all subscribers except the author.
	for name, ch := range c.subscribers {
		if name == author {
			continue
		}
		select {
		case ch <- msg:
		default:
			// Buffer full; drop the message for this subscriber.
			// The subscriber can catch up via History().
		}
	}

	return msg, nil
}

// Subscribe registers an agent to receive new messages. Returns a buffered
// channel that receives messages posted by other agents. If the agent is
// already subscribed, the existing channel is returned.
func (c *Channel) Subscribe(agentName string) <-chan Message {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ch, ok := c.subscribers[agentName]; ok {
		return ch
	}

	ch := make(chan Message, subscriberBufferSize)
	c.subscribers[agentName] = ch

	// Initialize cursor to current position so the agent doesn't see
	// pre-subscription messages as "unread" in Pending().
	if _, ok := c.cursors[agentName]; !ok {
		c.cursors[agentName] = c.nextTurn
	}

	return ch
}

// Unsubscribe removes an agent's subscription and closes their channel.
func (c *Channel) Unsubscribe(agentName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ch, ok := c.subscribers[agentName]; ok {
		close(ch)
		delete(c.subscribers, agentName)
	}
}

// MarkRead advances the read cursor for an agent up to the given turn number.
// The cursor only moves forward; passing a lower turn number is a no-op.
func (c *Channel) MarkRead(agentName string, upToTurn int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if current, ok := c.cursors[agentName]; !ok || upToTurn > current {
		c.cursors[agentName] = upToTurn
	}
}

// Pending returns the unread notification for an agent based on their cursor
// position. Returns a zero-value Notification if nothing is unread.
func (c *Channel) Pending(agentName string) Notification {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cursor := c.cursors[agentName] // defaults to 0 if not set
	unread := c.nextTurn - cursor
	if unread <= 0 {
		return Notification{}
	}

	// Read unread messages from the subscriber's channel metadata.
	// We can't peek at the Go channel, so we report count from cursors.
	// Authors are not tracked at the cursor level, so we return an empty
	// list here. Callers who need authors should use History().
	return Notification{
		UnreadCount: unread,
	}
}

// History returns all messages in order from the store. The store is the
// authoritative source of message history.
func (c *Channel) History() ([]Message, error) {
	storeMessages, err := c.store.GetDebateMessages(c.debateID)
	if err != nil {
		return nil, fmt.Errorf("loading history: %w", err)
	}

	messages := make([]Message, len(storeMessages))
	for i, sm := range storeMessages {
		messages[i] = Message{
			ID:        sm.ID,
			Author:    sm.Author,
			Content:   sm.Content,
			Timestamp: sm.Timestamp,
			TurnNum:   sm.TurnNum,
		}
	}
	return messages, nil
}

// Len returns the total number of messages posted to this channel.
func (c *Channel) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nextTurn
}
