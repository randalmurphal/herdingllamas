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

	"github.com/google/uuid"
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
	EventType string // nudge_sent, agent_started, agent_stopped, error, state_change
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
	// Set pragmas explicitly for the in-memory path (OpenInMemory) which
	// doesn't use DSN parameters. For the file-based path (Open), these
	// are already set via DSN but re-applying is harmless.
	pragmas := []string{
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

// --- Cursors ---

// PostMessage atomically assigns the next turn number and inserts a message.
// This is used by the CLI channel commands where the caller doesn't know the
// next turn number. The turn number is determined within a transaction to
// prevent races between concurrent posters.
func (s *Store) PostMessage(debateID, author, content string) (Message, error) {
	// Use BEGIN IMMEDIATE to acquire the write lock before the SELECT.
	// This prevents a TOCTOU race where two concurrent processes both
	// SELECT the same MAX(turn_num) then both INSERT with the same value.
	tx, err := s.db.Begin()
	if err != nil {
		return Message{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Acquire write lock immediately. In WAL mode, this serializes
	// concurrent writers from different processes.
	if _, err := tx.Exec("UPDATE debates SET id = id WHERE id = ?", debateID); err != nil {
		return Message{}, fmt.Errorf("acquiring write lock: %w", err)
	}

	var nextTurn int
	err = tx.QueryRow(
		"SELECT COALESCE(MAX(turn_num), -1) + 1 FROM messages WHERE debate_id = ?",
		debateID,
	).Scan(&nextTurn)
	if err != nil {
		return Message{}, fmt.Errorf("getting next turn number: %w", err)
	}

	id := uuid.New().String()
	now := time.Now().UTC()

	_, err = tx.Exec(
		`INSERT INTO messages (id, debate_id, author, content, timestamp, turn_num)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, debateID, author, content, now, nextTurn,
	)
	if err != nil {
		return Message{}, fmt.Errorf("inserting message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Message{}, fmt.Errorf("committing transaction: %w", err)
	}

	return Message{
		ID:        id,
		DebateID:  debateID,
		Author:    author,
		Content:   content,
		Timestamp: now,
		TurnNum:   nextTurn,
	}, nil
}

// GetUnreadCount returns the number of messages from other agents that this
// agent has not yet read. Used by the nudge loop to decide whether to send
// a notification. If no cursor exists, all non-self messages are unread.
func (s *Store) GetUnreadCount(debateID, agentName string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM messages
		WHERE debate_id = ?
		  AND author != ?
		  AND turn_num > COALESCE(
		      (SELECT last_read_turn FROM agent_cursors
		       WHERE debate_id = ? AND agent_name = ?),
		      -1
		  )
	`, debateID, agentName, debateID, agentName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting unread messages: %w", err)
	}
	return count, nil
}

// GetUnreadMessages returns all messages since the agent's read cursor,
// ordered by turn number. Includes the agent's own messages for full context.
// If no cursor exists, returns all messages in the debate.
func (s *Store) GetUnreadMessages(debateID, agentName string) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, debate_id, author, content, timestamp, turn_num
		FROM messages
		WHERE debate_id = ?
		  AND turn_num > COALESCE(
		      (SELECT last_read_turn FROM agent_cursors
		       WHERE debate_id = ? AND agent_name = ?),
		      -1
		  )
		ORDER BY turn_num ASC
	`, debateID, debateID, agentName)
	if err != nil {
		return nil, fmt.Errorf("getting unread messages: %w", err)
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

// UpdateCursor upserts the read cursor for an agent. The cursor only advances
// forward — passing a lower turn number than the current cursor is a no-op.
func (s *Store) UpdateCursor(debateID, agentName string, turnNum int) error {
	_, err := s.db.Exec(`
		INSERT INTO agent_cursors (debate_id, agent_name, last_read_turn)
		VALUES (?, ?, ?)
		ON CONFLICT(debate_id, agent_name) DO UPDATE
		SET last_read_turn = MAX(excluded.last_read_turn, agent_cursors.last_read_turn)
	`, debateID, agentName, turnNum)
	if err != nil {
		return fmt.Errorf("updating cursor for %s: %w", agentName, err)
	}
	return nil
}

// --- Conclusions ---

// SetConcluded records that an agent has proposed ending the debate.
// If the agent already has a conclusion recorded, this is a no-op (upsert).
func (s *Store) SetConcluded(debateID, agentName string) error {
	_, err := s.db.Exec(`
		INSERT INTO conclusions (debate_id, agent_name)
		VALUES (?, ?)
		ON CONFLICT(debate_id, agent_name) DO NOTHING
	`, debateID, agentName)
	if err != nil {
		return fmt.Errorf("setting concluded for %s in debate %s: %w", agentName, debateID, err)
	}
	return nil
}

// RevokeConcluded removes an agent's conclusion vote. This is called when
// an agent posts a new message after concluding (they changed their mind).
// If no conclusion exists, this is a no-op.
func (s *Store) RevokeConcluded(debateID, agentName string) error {
	_, err := s.db.Exec(`
		DELETE FROM conclusions
		WHERE debate_id = ? AND agent_name = ?
	`, debateID, agentName)
	if err != nil {
		return fmt.Errorf("revoking conclusion for %s in debate %s: %w", agentName, debateID, err)
	}
	return nil
}

// GetConcluded returns the list of agent names that have concluded in a debate.
func (s *Store) GetConcluded(debateID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT agent_name FROM conclusions
		WHERE debate_id = ?
		ORDER BY concluded_at ASC
	`, debateID)
	if err != nil {
		return nil, fmt.Errorf("getting conclusions for debate %s: %w", debateID, err)
	}
	defer rows.Close()

	var agents []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning conclusion row: %w", err)
		}
		agents = append(agents, name)
	}
	return agents, rows.Err()
}

// AllConcluded returns true if the number of conclusion votes for a debate
// is at least expectedAgents.
func (s *Store) AllConcluded(debateID string, expectedAgents int) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM conclusions WHERE debate_id = ?
	`, debateID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("counting conclusions for debate %s: %w", debateID, err)
	}
	return count >= expectedAgents, nil
}

// --- Status Queries ---

// AgentStatus contains the current state of an agent in a debate for display
// in channel tool output.
type AgentStatus struct {
	Name         string
	LastReadTurn int // -1 if never read
	LastPostTurn int // -1 if never posted
}

// GetAgentStatuses returns the status of all agents in a debate. Agent names
// are derived from the messages table (DISTINCT author excluding moderator and
// system) since the agents table may not be populated when CLI tools run.
func (s *Store) GetAgentStatuses(debateID string) ([]AgentStatus, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT author FROM messages
		WHERE debate_id = ? AND author NOT IN ('moderator', 'system')
		ORDER BY author ASC
	`, debateID)
	if err != nil {
		return nil, fmt.Errorf("getting agent names for debate %s: %w", debateID, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning agent name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating agent names: %w", err)
	}

	statuses := make([]AgentStatus, 0, len(names))
	for _, name := range names {
		status := AgentStatus{
			Name:         name,
			LastReadTurn: -1,
			LastPostTurn: -1,
		}

		// Get last_read_turn from agent_cursors.
		var lastRead sql.NullInt64
		err := s.db.QueryRow(`
			SELECT last_read_turn FROM agent_cursors
			WHERE debate_id = ? AND agent_name = ?
		`, debateID, name).Scan(&lastRead)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("getting cursor for %s: %w", name, err)
		}
		if lastRead.Valid {
			status.LastReadTurn = int(lastRead.Int64)
		}

		// Get max turn_num from messages (last post).
		var lastPost sql.NullInt64
		err = s.db.QueryRow(`
			SELECT MAX(turn_num) FROM messages
			WHERE debate_id = ? AND author = ?
		`, debateID, name).Scan(&lastPost)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("getting last post for %s: %w", name, err)
		}
		if lastPost.Valid {
			status.LastPostTurn = int(lastPost.Int64)
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// --- Wait-oriented Queries ---

// GetLatestTurnNum returns the highest turn number in a debate, or -1 if empty.
func (s *Store) GetLatestTurnNum(debateID string) (int, error) {
	var turnNum sql.NullInt64
	err := s.db.QueryRow(`
		SELECT MAX(turn_num) FROM messages WHERE debate_id = ?
	`, debateID).Scan(&turnNum)
	if err != nil {
		return -1, fmt.Errorf("getting latest turn for debate %s: %w", debateID, err)
	}
	if !turnNum.Valid {
		return -1, nil
	}
	return int(turnNum.Int64), nil
}

// GetMessagesAfterTurn returns messages with turn_num > afterTurn from authors
// other than excludeAuthor.
func (s *Store) GetMessagesAfterTurn(debateID string, afterTurn int, excludeAuthor string) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, debate_id, author, content, timestamp, turn_num
		FROM messages
		WHERE debate_id = ?
		  AND turn_num > ?
		  AND author != ?
		  AND author NOT IN ('moderator', 'system')
		ORDER BY turn_num ASC
	`, debateID, afterTurn, excludeAuthor)
	if err != nil {
		return nil, fmt.Errorf("getting messages after turn %d: %w", afterTurn, err)
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

// HasCursorAdvancedPast checks if any agent other than the given one has a
// cursor >= turnNum. Returns (agentName, true, nil) if someone has read past
// turnNum, or ("", false, nil) if not.
func (s *Store) HasCursorAdvancedPast(debateID, excludeAgent string, turnNum int) (string, bool, error) {
	var name string
	err := s.db.QueryRow(`
		SELECT agent_name FROM agent_cursors
		WHERE debate_id = ?
		  AND agent_name != ?
		  AND last_read_turn >= ?
		LIMIT 1
	`, debateID, excludeAgent, turnNum).Scan(&name)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("checking cursor advancement: %w", err)
	}
	return name, true, nil
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
