DROP INDEX IF EXISTS idx_deliveries_external;
DROP INDEX IF EXISTS idx_projects_external;
ALTER TABLE deliveries DROP COLUMN IF EXISTS priority;
ALTER TABLE deliveries DROP COLUMN IF EXISTS assignee;
ALTER TABLE deliveries DROP COLUMN IF EXISTS external_synced_at;
ALTER TABLE deliveries DROP COLUMN IF EXISTS external_issue_key;
ALTER TABLE deliveries DROP COLUMN IF EXISTS external_issue_id;
ALTER TABLE projects DROP COLUMN IF EXISTS external_synced_at;
ALTER TABLE projects DROP COLUMN IF EXISTS external_project_id;
