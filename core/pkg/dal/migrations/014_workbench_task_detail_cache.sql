CREATE TABLE workbench_task_detail_cache (
  task_id TEXT NOT NULL REFERENCES workbench_tasks(id) ON DELETE CASCADE,
  segment TEXT NOT NULL,
  page_key TEXT NOT NULL,
  source_version TEXT NOT NULL,
  auth_generation INTEGER NOT NULL,
  config_revision INTEGER NOT NULL,
  package_digest TEXT NOT NULL,
  captured_at TEXT NOT NULL,
  payload TEXT NOT NULL,
  PRIMARY KEY(task_id,segment,page_key)
);
