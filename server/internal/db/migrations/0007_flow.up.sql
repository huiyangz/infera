-- 需求流转（INFERA-11 T01，契约冻结）：infera 作为唯一前台的大节点状态源。
-- 表结构是 flow 包领域类型的落库形态，下游（gatepoll / reqservice / API）只读消费。

CREATE TABLE requirements (
    id                  UUID PRIMARY KEY,
    title               TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',        -- 业务描述（只存 infera）
    acceptance_criteria TEXT NOT NULL DEFAULT '',        -- 验收标准（只存 infera）
    source              TEXT NOT NULL DEFAULT '',
    priority            TEXT NOT NULL DEFAULT '',
    acceptors           TEXT[] NOT NULL DEFAULT '{}',    -- 验收人
    external_issue_id    TEXT NOT NULL DEFAULT '',        -- 上游 issue id 映射
    external_issue_key   TEXT NOT NULL DEFAULT '',        -- 如 INFERA-31
    node                TEXT NOT NULL DEFAULT 'intake',  -- 大节点（flow.Node slug）
    pr_url              TEXT NOT NULL DEFAULT '',        -- 评论提取的 github PR 引用
    -- 轮询位置（flow.PollCursor，1:1 挂在需求行上）
    poll_last_comment_at TIMESTAMPTZ,                    -- since 游标；NULL = 尚未轮询
    poll_last_status     TEXT NOT NULL DEFAULT '',       -- 上次见到的上游状态
    poll_seen_verdict    BOOLEAN NOT NULL DEFAULT FALSE, -- 是否见过 verdict 评论
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_requirements_external_issue ON requirements(external_issue_id) WHERE external_issue_id <> '';
CREATE INDEX idx_requirements_node ON requirements(node);

CREATE TABLE gate_cards (
    id             UUID PRIMARY KEY,
    requirement_id UUID NOT NULL REFERENCES requirements(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL,                          -- approval/decision/merge/update
    status         TEXT NOT NULL DEFAULT 'pending',        -- pending/resolved
    payload        TEXT NOT NULL DEFAULT '',               -- 卡片渲染正文
    comment_id     TEXT NOT NULL DEFAULT '',               -- 溯源评论（状态类兜底卡为空）
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at    TIMESTAMPTZ
);
CREATE INDEX idx_gate_cards_requirement ON gate_cards(requirement_id);
CREATE INDEX idx_gate_cards_pending ON gate_cards(requirement_id, status);

-- 审计是只增不改的动作记录：不挂 CASCADE——删除带审计历史的需求应报错，
-- 而不是悄悄抹掉轨迹。
CREATE TABLE audit_log (
    id             UUID PRIMARY KEY,
    requirement_id UUID NOT NULL REFERENCES requirements(id),
    actor          TEXT NOT NULL,   -- 谁
    action         TEXT NOT NULL,   -- 做了什么（approve/reject/decide/rework/merge/...）
    detail         TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_log_requirement ON audit_log(requirement_id);

-- 项目级设置（FR-6 合并策略档位）。
CREATE TABLE project_settings (
    project_id                 UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    merge_policy_mode          TEXT NOT NULL DEFAULT 'manual', -- manual/auto_pass/threshold
    merge_diff_line_threshold  INT NOT NULL DEFAULT 0,         -- 仅 threshold 档有意义
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);
