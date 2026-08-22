DROP INDEX IF EXISTS idx_deliveries_multica;
DROP INDEX IF EXISTS idx_projects_multica;
ALTER TABLE deliveries DROP COLUMN IF EXISTS priority;
ALTER TABLE deliveries DROP COLUMN IF EXISTS assignee;
ALTER TABLE deliveries DROP COLUMN IF EXISTS multica_synced_at;
ALTER TABLE deliveries DROP COLUMN IF EXISTS multica_issue_key;
ALTER TABLE deliveries DROP COLUMN IF EXISTS multica_issue_id;
ALTER TABLE projects DROP COLUMN IF EXISTS multica_synced_at;
ALTER TABLE projects DROP COLUMN IF EXISTS multica_project_id;
