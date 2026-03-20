// Package store provides SQLite persistence for debate data including
// debates, messages, agents, and events.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps a SQLite database connection for debate persistence.
type Store struct {
	db *sql.DB
}

// Debate represents a single debate session.
type Debate struct {
	ID        string
	Question  string
	Config    string // JSON-serialized debate config
	Status    string // active, completed, cancelled, error
	CreatedAt time.Time
	EndedAt   *time.Time
}

// Message represents a single message within a debate.
type Message struct {
	ID        string
	DebateID  string
	Author    string // agent name: "claude", "codex", "system"
	Content   string
	Timestamp time.Time
	TurnNum   int
}

// Agent represents an AI agent participating in a debate.
type Agent struct {
	ID           string
	DebateID     string
	Name         string // "claude", "codex"
	Provider     string
	Model        string
	SessionID    string
	Status       string // starting, active, waiting, stopped, error
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Event represents a discrete event that occurred during a debate.
type Event struct {
	ID        int64
	DebateID  string
	EventType string // nudge_sent, hook_fired, agent_started, agent_stopped, error, state_change
	AgentName string
	Payload   string // JSON payload with event-specific data
	Timestamp time.Time
}

// Open opens or creates the SQLite database at the given path.
// Creates parent directories if needed and applies the schema on first open.
func Open(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating database directory %s: %w", dir, err)
	}

	// Use _pragma DSN parameters so pragmas are applied per-connection,
	// not just once on the first connection from the pool.
	dsn := dbPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dbPath, err)
	}

	// Single-writer CLI tool: one connection avoids pragma-per-connection issues.
	db.SetMaxOpenConns(1)

	if err := applySchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &Store{db: db}, nil
}

// OpenInMemory creates an in-memory SQLite database with the schema applied.
// Useful for testing. The connection pool is limited to one connection because
// each ":memory:" connection gets its own independent database.
func OpenInMemory() (*Store, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("opening in-memory database: %w", err)
	}

	// In-memory databases are per-connection, so we must use exactly one
	// connection to ensure all operations see the same schema and data.
	db.SetMaxOpenConns(1)

	if err := applySchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DefaultDBPath returns the default database path: ~/.herdingllamas/debates.db
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".herdingllamas", "debates.db"), nil
}

func applySchema(db *sql.DB) error {
	// Enable WAL mode and foreign keys for better concurrent access.
	// busy_timeout prevents immediate SQLITE_BUSY errors under contention.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("executing %s: %w", p, err)
		}
	}

	_, err := db.Exec(schemaSQL)
	if err != nil {
		return fmt.Errorf("executing schema SQL: %w", err)
	}
	return nil
}

// --- Debates ---

// InsertDebate inserts a new debate record.
func (s *Store) InsertDebate(d Debate) error {
	_, err := s.db.Exec(
		`INSERT INTO debates (id, question, config, status, created_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.Question, d.Config, d.Status, d.CreatedAt, d.EndedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting debate %s: %w", d.ID, err)
	}
	return nil
}

// UpdateDebateStatus updates the status of a debate.
func (s *Store) UpdateDebateStatus(id, status string) error {
	result, err := s.db.Exec(
		`UPDATE debates SET status = ? WHERE id = ?`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("updating debate status %s: %w", id, err)
	}
	return checkRowAffected(result, "debate", id)
}

// EndDebate sets the debate status and records the ended_at timestamp.
func (s *Store) EndDebate(id, status string) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(
		`UPDATE debates SET status = ?, ended_at = ? WHERE id = ?`,
		status, now, id,
	)
	if err != nil {
		return fmt.Errorf("ending debate %s: %w", id, err)
	}
	return checkRowAffected(result, "debate", id)
}

// GetDebate retrieves a single debate by ID. Returns nil if not found.
func (s *Store) GetDebate(id string) (*Debate, error) {
	var d Debate
	err := s.db.QueryRow(
		`SELECT id, question, config, status, created_at, ended_at
		 FROM debates WHERE id = ?`, id,
	).Scan(&d.ID, &d.Question, &d.Config, &d.Status, &d.CreatedAt, &d.EndedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting debate %s: %w", id, err)
	}
	return &d, nil
}

// ListDebates returns all debates ordered by creation time (newest first).
func (s *Store) ListDebates() ([]Debate, error) {
	rows, err := s.db.Query(
		`SELECT id, question, config, status, created_at, ended_at
		 FROM debates ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing debates: %w", err)
	}
	defer rows.Close()

	var debates []Debate
	for rows.Next() {
		var d Debate
		if err := rows.Scan(&d.ID, &d.Question, &d.Config, &d.Status, &d.CreatedAt, &d.EndedAt); err != nil {
			return nil, fmt.Errorf("scanning debate row: %w", err)
		}
		debates = append(debates, d)
	}
	return debates, rows.Err()
}

// --- Messages ---

// InsertMessage inserts a new message record.
func (s *Store) InsertMessage(m Message) error {
	_, err := s.db.Exec(
		`INSERT INTO messages (id, debate_id, author, content, timestamp, turn_num)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, m.DebateID, m.Author, m.Content, m.Timestamp, m.TurnNum,
	)
	if err != nil {
		return fmt.Errorf("inserting message %s: %w", m.ID, err)
	}
	return nil
}

// GetDebateMessages returns all messages for a debate ordered by turn number.
func (s *Store) GetDebateMessages(debateID string) ([]Message, error) {
	rows, err := s.db.Query(
		`SELECT id, debate_id, author, content, timestamp, turn_num
		 FROM messages WHERE debate_id = ? ORDER BY turn_num ASC`,
		debateID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting messages for debate %s: %w", debateID, err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.DebateID, &m.Author, &m.Content, &m.Timestamp, &m.TurnNum); err != nil {
			return nil, fmt.Errorf("scanning message row: %w", err)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

// GetMessageCount returns the total number of messages in a debate.
func (s *Store) GetMessageCount(debateID string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE debate_id = ?`, debateID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting messages for debate %s: %w", debateID, err)
	}
	return count, nil
}

// --- Agents ---

// InsertAgent inserts a new agent record.
func (s *Store) InsertAgent(a Agent) error {
	_, err := s.db.Exec(
		`INSERT INTO agents (id, debate_id, name, provider, model, session_id, status,
		                     input_tokens, output_tokens, cost_usd, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.DebateID, a.Name, a.Provider, a.Model, a.SessionID, a.Status,
		a.InputTokens, a.OutputTokens, a.CostUSD, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("inserting agent %s: %w", a.ID, err)
	}
	return nil
}

// UpdateAgentStatus updates the status and updated_at timestamp for an agent.
func (s *Store) UpdateAgentStatus(id, status string) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(
		`UPDATE agents SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id,
	)
	if err != nil {
		return fmt.Errorf("updating agent status %s: %w", id, err)
	}
	return checkRowAffected(result, "agent", id)
}

// UpdateAgentUsage updates the token counts and cost for an agent.
func (s *Store) UpdateAgentUsage(id string, inputTokens, outputTokens int, costUSD float64) error {
	now := time.Now().UTC()
	result, err := s.db.Exec(
		`UPDATE agents SET input_tokens = ?, output_tokens = ?, cost_usd = ?, updated_at = ?
		 WHERE id = ?`,
		inputTokens, outputTokens, costUSD, now, id,
	)
	if err != nil {
		return fmt.Errorf("updating agent usage %s: %w", id, err)
	}
	return checkRowAffected(result, "agent", id)
}

// GetDebateAgents returns all agents for a debate.
func (s *Store) GetDebateAgents(debateID string) ([]Agent, error) {
	rows, err := s.db.Query(
		`SELECT id, debate_id, name, provider, model, session_id, status,
		        input_tokens, output_tokens, cost_usd, created_at, updated_at
		 FROM agents WHERE debate_id = ?`,
		debateID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting agents for debate %s: %w", debateID, err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		if err := rows.Scan(
			&a.ID, &a.DebateID, &a.Name, &a.Provider, &a.Model, &a.SessionID,
			&a.Status, &a.InputTokens, &a.OutputTokens, &a.CostUSD,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning agent row: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// --- Events ---

// InsertEvent inserts a new event record.
func (s *Store) InsertEvent(e Event) error {
	_, err := s.db.Exec(
		`INSERT INTO events (debate_id, event_type, agent_name, payload, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		e.DebateID, e.EventType, e.AgentName, e.Payload, e.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("inserting event for debate %s: %w", e.DebateID, err)
	}
	return nil
}

// GetDebateEvents returns all events for a debate ordered by timestamp.
func (s *Store) GetDebateEvents(debateID string) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT id, debate_id, event_type, agent_name, payload, timestamp
		 FROM events WHERE debate_id = ? ORDER BY timestamp ASC, id ASC`,
		debateID,
	)
	if err != nil {
		return nil, fmt.Errorf("getting events for debate %s: %w", debateID, err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// GetDebateEventsByType returns events for a debate filtered by event type.
func (s *Store) GetDebateEventsByType(debateID, eventType string) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT id, debate_id, event_type, agent_name, payload, timestamp
		 FROM events WHERE debate_id = ? AND event_type = ?
		 ORDER BY timestamp ASC, id ASC`,
		debateID, eventType,
	)
	if err != nil {
		return nil, fmt.Errorf("getting events type %s for debate %s: %w", eventType, debateID, err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var events []Event
	for rows.Next() {
		var e Event
		var agentName sql.NullString
		var payload sql.NullString
		if err := rows.Scan(&e.ID, &e.DebateID, &e.EventType, &agentName, &payload, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}
		e.AgentName = agentName.String
		e.Payload = payload.String
		events = append(events, e)
	}
	return events, rows.Err()
}

// checkRowAffected verifies that an update modified exactly one row.
func checkRowAffected(result sql.Result, entity, id string) error {
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected for %s %s: %w", entity, id, err)
	}
	if n == 0 {
		return fmt.Errorf("%s not found: %s", entity, id)
	}
	return nil
}
