CREATE TABLE workbench_connections (
  id TEXT PRIMARY KEY,
  config TEXT NOT NULL,
  version INTEGER NOT NULL,
  last_success TEXT,
  attempts TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE workbench_objects (
  id TEXT PRIMARY KEY,
  connection_id TEXT NOT NULL REFERENCES workbench_connections(id) ON DELETE CASCADE,
  source_key TEXT NOT NULL,
  title TEXT NOT NULL,
  state TEXT NOT NULL,
  payload TEXT NOT NULL,
  UNIQUE (connection_id, source_key)
);
CREATE INDEX workbench_object_list ON workbench_objects(connection_id, state, title, id);
CREATE TABLE workbench_revision (revision INTEGER NOT NULL);
INSERT INTO workbench_revision VALUES (1);
