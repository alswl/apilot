CREATE TABLE workbench_connector_connections (
  id TEXT PRIMARY KEY NOT NULL,
  connector_id TEXT NOT NULL,
  endpoint_ref TEXT NOT NULL,
  account_ref TEXT NOT NULL,
  auth_state TEXT NOT NULL DEFAULT 'unknown',
  auth_generation INTEGER NOT NULL DEFAULT 0 CHECK (auth_generation >= 0),
  UNIQUE (connector_id, endpoint_ref, account_ref)
);

ALTER TABLE workbench_connections ADD COLUMN connector_connection_id TEXT
  REFERENCES workbench_connector_connections(id) ON DELETE RESTRICT;
ALTER TABLE workbench_connections ADD COLUMN scope_ref TEXT CHECK (
  (connector_connection_id IS NULL AND scope_ref IS NULL)
  OR (connector_connection_id IS NOT NULL AND scope_ref IS NOT NULL AND length(scope_ref) > 0)
);
CREATE UNIQUE INDEX workbench_connections_connector_scope
  ON workbench_connections(connector_connection_id, scope_ref);
ALTER TABLE workbench_connections ADD COLUMN access_state TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE workbench_connections ADD COLUMN config_revision INTEGER NOT NULL DEFAULT 0 CHECK (config_revision >= 0);
ALTER TABLE workbench_connections ADD COLUMN mapping_revision INTEGER NOT NULL DEFAULT 0 CHECK (mapping_revision >= 0);
ALTER TABLE workbench_connections ADD COLUMN sync_checkpoint TEXT;
ALTER TABLE workbench_connections ADD COLUMN last_full_success TEXT;

CREATE TABLE workbench_sync_runs (
  id TEXT PRIMARY KEY NOT NULL,
  scope_id TEXT NOT NULL REFERENCES workbench_connections(id) ON DELETE CASCADE,
  trigger TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('full', 'delta')),
  status TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  duration_ms INTEGER CHECK (duration_ms >= 0),
  failed_stage TEXT,
  error_kind TEXT,
  retry_at TEXT,
  requests INTEGER NOT NULL DEFAULT 0 CHECK (requests >= 0),
  pages INTEGER NOT NULL DEFAULT 0 CHECK (pages >= 0),
  fetched INTEGER NOT NULL DEFAULT 0 CHECK (fetched >= 0),
  changed INTEGER NOT NULL DEFAULT 0 CHECK (changed >= 0),
  rate_limits INTEGER NOT NULL DEFAULT 0 CHECK (rate_limits >= 0),
  usage_complete INTEGER NOT NULL DEFAULT 0 CHECK (usage_complete IN (0, 1))
);
CREATE INDEX workbench_sync_runs_scope_started ON workbench_sync_runs(scope_id, started_at);
CREATE INDEX workbench_sync_runs_started ON workbench_sync_runs(started_at);

ALTER TABLE workbench_plugin_settings ADD COLUMN release_version TEXT;
ALTER TABLE workbench_plugin_settings ADD COLUMN protocol_version INTEGER;
ALTER TABLE workbench_plugin_settings ADD COLUMN package_digest TEXT;
