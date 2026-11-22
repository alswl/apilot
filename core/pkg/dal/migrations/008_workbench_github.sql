CREATE TABLE IF NOT EXISTS workbench_github_connections (
  connection_id TEXT PRIMARY KEY NOT NULL REFERENCES workbench_connections(id) ON DELETE CASCADE,
  repository_id INTEGER NOT NULL UNIQUE,
  account_id INTEGER NOT NULL,
  account_login TEXT NOT NULL,
  credential_ref TEXT NOT NULL,
  access_state TEXT NOT NULL,
  cursor TEXT,
  last_full_sync TEXT
);
