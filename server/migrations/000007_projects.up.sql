CREATE TABLE projects (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL,
    repo_url       text NOT NULL DEFAULT '',
    default_branch text NOT NULL DEFAULT 'main',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- 回填现有 deliveries 到一个 Default 项目
INSERT INTO projects (name, repo_url) VALUES ('Default', '');

ALTER TABLE deliveries ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE CASCADE;
UPDATE deliveries SET project_id = (SELECT id FROM projects WHERE name = 'Default' LIMIT 1);
ALTER TABLE deliveries ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE deliveries DROP COLUMN repo_url;
ALTER TABLE deliveries DROP COLUMN branch;
