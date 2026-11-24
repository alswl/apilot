ALTER TABLE workbench_connector_connections ADD COLUMN endpoint_config TEXT NOT NULL DEFAULT '{}';
ALTER TABLE workbench_connector_connections ADD COLUMN endpoint_schema_version INTEGER NOT NULL DEFAULT 1;

UPDATE workbench_connector_connections AS parent
SET (endpoint_config, endpoint_schema_version) = (
  SELECT coalesce(child.config -> '$.endpoint_config', '{}'),
         coalesce(child.config ->> '$.endpoint_schema_version', 1)
  FROM workbench_connections AS child
  WHERE child.connector_connection_id = parent.id
  ORDER BY child.id
  LIMIT 1
)
WHERE EXISTS (SELECT 1 FROM workbench_connections WHERE connector_connection_id = parent.id);

UPDATE workbench_connections
SET config = json_remove(config, '$.endpoint_config', '$.endpoint_schema_version')
WHERE connector_connection_id IS NOT NULL;
