ALTER TABLE workbench_connections ADD COLUMN snapshot_version INTEGER NOT NULL DEFAULT 0;

ALTER TABLE workbench_task_writes ADD COLUMN input TEXT;
ALTER TABLE workbench_task_writes ADD COLUMN phase TEXT NOT NULL DEFAULT 'legacy'
  CHECK (phase IN ('legacy', 'accepted', 'dispatching', 'needs_review', 'confirmed', 'conflict', 'not_executed'));
CREATE INDEX workbench_task_writes_recovery ON workbench_task_writes(phase, connection_id);

CREATE TABLE workbench_tasks (
  id TEXT PRIMARY KEY NOT NULL,
  created_at TEXT NOT NULL
);

INSERT INTO workbench_tasks (id, created_at)
SELECT id, strftime('%Y-%m-%dT%H:%M:%fZ', 'now') FROM workbench_objects;

CREATE TABLE workbench_objects_v6 (
  id TEXT PRIMARY KEY NOT NULL REFERENCES workbench_tasks(id) ON DELETE CASCADE,
  connection_id TEXT NOT NULL REFERENCES workbench_connections(id) ON DELETE CASCADE,
  source_key TEXT NOT NULL,
  title TEXT NOT NULL,
  state TEXT NOT NULL,
  payload TEXT NOT NULL,
  UNIQUE (connection_id, source_key)
);
INSERT INTO workbench_objects_v6 (id, connection_id, source_key, title, state, payload)
SELECT id, connection_id, source_key, title, state, payload FROM workbench_objects;
DROP TABLE workbench_objects;
ALTER TABLE workbench_objects_v6 RENAME TO workbench_objects;
CREATE INDEX workbench_object_list ON workbench_objects(connection_id, state, title, id);
