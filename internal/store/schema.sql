PRAGMA user_version = 1;

CREATE TABLE IF NOT EXISTS debates (
    id TEXT PRIMARY KEY,
    question TEXT NOT NULL,
    config TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME
);

CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    debate_id TEXT NOT NULL REFERENCES debates(id),
    author TEXT NOT NULL,
    content TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    turn_num INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_debate_turn ON messages(debate_id, turn_num);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    debate_id TEXT NOT NULL REFERENCES debates(id),
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'starting',
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd REAL NOT NULL DEFAULT 0.0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agents_debate ON agents(debate_id);

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    debate_id TEXT NOT NULL REFERENCES debates(id),
    event_type TEXT NOT NULL,
    agent_name TEXT,
    payload TEXT,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_events_debate ON events(debate_id, timestamp);

CREATE TABLE IF NOT EXISTS agent_cursors (
    debate_id TEXT NOT NULL REFERENCES debates(id),
    agent_name TEXT NOT NULL,
    last_read_turn INTEGER NOT NULL DEFAULT -1,
    PRIMARY KEY (debate_id, agent_name)
);

CREATE TABLE IF NOT EXISTS conclusions (
    debate_id TEXT NOT NULL REFERENCES debates(id),
    agent_name TEXT NOT NULL,
    concluded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (debate_id, agent_name)
);
