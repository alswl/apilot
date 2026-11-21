CREATE TABLE workbench_labels (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL UNIQUE CHECK (length(name) > 0),
  created_at TEXT NOT NULL
);
CREATE TABLE workbench_task_labels (
  task_id TEXT NOT NULL REFERENCES workbench_tasks(id) ON DELETE CASCADE,
  label_id TEXT NOT NULL REFERENCES workbench_labels(id) ON DELETE CASCADE,
  PRIMARY KEY (task_id, label_id)
);
CREATE INDEX workbench_task_labels_by_label ON workbench_task_labels(label_id, task_id);
CREATE TABLE workbench_task_annotations (
  id TEXT PRIMARY KEY NOT NULL,
  task_id TEXT NOT NULL REFERENCES workbench_tasks(id) ON DELETE CASCADE,
  body TEXT NOT NULL CHECK (length(body) > 0),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX workbench_task_annotations_by_task ON workbench_task_annotations(task_id, created_at, id);
