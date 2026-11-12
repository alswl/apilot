CREATE TABLE tasks (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, payload TEXT NOT NULL, env_json TEXT NOT NULL);
CREATE TABLE schedules (id TEXT PRIMARY KEY, task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id) ON DELETE CASCADE, payload TEXT NOT NULL);
CREATE TABLE runs (id TEXT PRIMARY KEY, task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, payload TEXT NOT NULL);
CREATE UNIQUE INDEX one_active_run ON runs(task_id) WHERE status IN ('queued','running');
CREATE INDEX runs_by_task ON runs(task_id,created_at DESC,id DESC);
CREATE INDEX recent_runs ON runs(created_at DESC,id DESC);
CREATE TABLE settings (id INTEGER PRIMARY KEY CHECK(id=1), payload TEXT NOT NULL);
CREATE TABLE attention_events (id TEXT PRIMARY KEY, payload TEXT NOT NULL);
