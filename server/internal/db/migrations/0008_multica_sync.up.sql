-- Multica 来源映射（INFERA-77 同步链路的存储面，契约冻结于 T02）：
-- 外部 ID 部分唯一（空串 = 非 multica 来源，不参与唯一性；同步 upsert 的 ON CONFLICT 目标）；
-- multica_synced_at NULL = 从未同步。
ALTER TABLE projects ADD COLUMN multica_project_id TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN multica_synced_at TIMESTAMPTZ;
CREATE UNIQUE INDEX idx_projects_multica ON projects(multica_project_id) WHERE multica_project_id <> '';

ALTER TABLE deliveries ADD COLUMN multica_issue_id TEXT NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN multica_issue_key TEXT NOT NULL DEFAULT ''; -- 展示键（如 INFERA-79）
ALTER TABLE deliveries ADD COLUMN multica_synced_at TIMESTAMPTZ;
-- 负责人/优先级：multica 同步进来的展示数据（非同步行为空）。
ALTER TABLE deliveries ADD COLUMN assignee TEXT NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN priority TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_deliveries_multica ON deliveries(multica_issue_id) WHERE multica_issue_id <> '';
