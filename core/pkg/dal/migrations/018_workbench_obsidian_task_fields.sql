-- Obsidian's shared Markdown parser already validated the stored native values.
-- Use the same priority order as the parser, and preserve task identity and versions.
WITH backfill AS MATERIALIZED (
    SELECT id,
           COALESCE(NULLIF(due_date, ''), json_extract(payload, '$.native_fields.due')) AS projected_due,
           COALESCE(priority, CASE json_extract(payload, '$.native_fields.priority')
               WHEN 'lowest' THEN 1
               WHEN 'low' THEN 2
               WHEN 'normal' THEN 3
               WHEN 'medium' THEN 4
               WHEN 'high' THEN 5
               WHEN 'highest' THEN 6
           END) AS projected_priority
    FROM workbench_objects
    WHERE source_kind = 'obsidian_tasks'
)
UPDATE workbench_objects AS task
SET payload = json_patch(task.payload, json_object(
    'due_date', backfill.projected_due,
    'priority', backfill.projected_priority
))
FROM backfill
WHERE task.id = backfill.id
  AND ((NULLIF(task.due_date, '') IS NULL AND backfill.projected_due IS NOT NULL)
       OR (task.priority IS NULL AND backfill.projected_priority IS NOT NULL));

UPDATE workbench_revision SET revision = revision + 1 WHERE changes() > 0;
