UPDATE workbench_objects
SET payload = json_set(payload, '$.kind', 'local', '$.mode', 'local')
WHERE connection_id IN (
    SELECT id FROM workbench_connections WHERE json_extract(config, '$.kind') = 'local_import'
);

UPDATE workbench_connections
SET config = json_set(config, '$.kind', 'local', '$.mode', 'local')
WHERE json_extract(config, '$.kind') = 'local_import';

WITH RECURSIVE names(suffix, name) AS (
    VALUES (1, 'Local')
    UNION ALL
    SELECT suffix + 1, 'Local ' || (suffix + 1)
    FROM names
    WHERE EXISTS (
        SELECT 1 FROM workbench_connections
        WHERE json_extract(config, '$.name') = names.name COLLATE NOCASE
    )
)
INSERT INTO workbench_connections (id, config, version)
SELECT 'local', json_object('id', 'local', 'kind', 'local', 'mode', 'local', 'version', 1, 'name', name), 1
FROM names ORDER BY suffix DESC LIMIT 1;

UPDATE workbench_revision SET revision = revision + 1;
