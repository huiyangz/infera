ALTER TABLE deliveries ADD COLUMN repo_url text NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN branch text NOT NULL DEFAULT '';
ALTER TABLE deliveries DROP COLUMN project_id;
DROP TABLE projects;
