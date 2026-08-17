-- Agent 注册表（全局）+ 节点→Agent 绑定（project_id 空 = 全局默认，非空 = 项目覆盖）。
CREATE TABLE agents (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    runner     TEXT NOT NULL,                -- cli | http | docker | local
    config     JSONB NOT NULL DEFAULT '{}',  -- {command:[...]} | {url} | {image,command} | {}
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE pipeline_bindings (
    id         UUID PRIMARY KEY,
    project_id UUID NULL REFERENCES projects(id) ON DELETE CASCADE,
    node       TEXT NOT NULL,                -- spec|test_gen|code_gen|code_review（design/tasks 后续批次）
    agent_id   UUID NOT NULL REFERENCES agents(id),  -- RESTRICT：仍被绑定的 agent 不许删（API → 409）
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- NULL 语义的 (project_id,node) 唯一：用部分唯一索引保证 PG 兼容。
CREATE UNIQUE INDEX idx_bindings_default ON pipeline_bindings(node) WHERE project_id IS NULL;
CREATE UNIQUE INDEX idx_bindings_project ON pipeline_bindings(project_id, node) WHERE project_id IS NOT NULL;
