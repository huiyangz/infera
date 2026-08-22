-- 外部来源映射（INFERA-77 同步链路的存储面，契约冻结于 T02）：
-- 外部 ID 部分唯一（空串 = 非同步来源，不参与唯一性；同步 upsert 的 ON CONFLICT 目标）；
-- external_synced_at NULL = 从未同步。
ALTER TABLE projects ADD COLUMN external_project_id TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN external_synced_at TIMESTAMPTZ;
CREATE UNIQUE INDEX idx_projects_external ON projects(external_project_id) WHERE external_project_id <> '';

ALTER TABLE deliveries ADD COLUMN external_issue_id TEXT NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN external_issue_key TEXT NOT NULL DEFAULT ''; -- 展示键（如 INFERA-79）
ALTER TABLE deliveries ADD COLUMN external_synced_at TIMESTAMPTZ;
-- 负责人/优先级：任务同步进来的展示数据（非同步行为空）。
ALTER TABLE deliveries ADD COLUMN assignee TEXT NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN priority TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_deliveries_external ON deliveries(external_issue_id) WHERE external_issue_id <> '';
