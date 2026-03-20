// Package channel provides message pub/sub with SQLite persistence for
// debate agents.
package channel

import "time"

// Message represents a single message exchanged between agents in a debate.
type Message struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	TurnNum   int       `json:"turn_num"`
}

// Notification summarizes unread messages for an agent.
type Notification struct {
	UnreadCount int `json:"unread_count"`
}
