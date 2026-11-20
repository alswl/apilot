CREATE TABLE workbench_plugin_settings (
  id TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL CHECK (enabled IN (0, 1))
);

CREATE TABLE workbench_task_writes (
  request_id TEXT PRIMARY KEY,
  connection_id TEXT NOT NULL REFERENCES workbench_connections(id) ON DELETE CASCADE,
  fingerprint TEXT NOT NULL,
  receipt TEXT NOT NULL
);

CREATE TABLE workbench_obsidian_bridges (
  connection_id TEXT PRIMARY KEY REFERENCES workbench_connections(id) ON DELETE CASCADE,
  endpoint TEXT NOT NULL,
  token TEXT NOT NULL,
  certificate TEXT NOT NULL
);
