CREATE TABLE observers (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, payload TEXT NOT NULL, env_json TEXT NOT NULL);
CREATE TABLE notification_limits (source_key TEXT PRIMARY KEY, last_sent_at TEXT NOT NULL);
