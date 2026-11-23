ALTER TABLE workbench_objects ADD COLUMN source_kind TEXT GENERATED ALWAYS AS (json_extract(payload,'$.kind')) VIRTUAL;
ALTER TABLE workbench_objects ADD COLUMN priority INTEGER GENERATED ALWAYS AS (json_extract(payload,'$.priority')) VIRTUAL;
ALTER TABLE workbench_objects ADD COLUMN due_date TEXT GENERATED ALWAYS AS (json_extract(payload,'$.due_date')) VIRTUAL;
CREATE INDEX workbench_objects_scope_due ON workbench_objects(connection_id,due_date,id);
CREATE INDEX workbench_objects_scope_priority ON workbench_objects(connection_id,priority,id);
CREATE INDEX workbench_objects_source_kind ON workbench_objects(source_kind,connection_id);
