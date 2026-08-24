-- 标签能力（INFERA-218 T01，契约冻结）：workspace 级标签库 + 交付↔标签关联。
-- external_label_id 是同步 upsert 的幂等键（上游标签 id；空串 = 非同步来源，
-- 不参与唯一性）——重复导入按它命中既有行，不产生重复标签。
-- color 存上游 hex 原值（如 #22c55e），不做色彩换算。

CREATE TABLE labels (
    id                UUID PRIMARY KEY,
    name              TEXT NOT NULL,
    color             TEXT NOT NULL DEFAULT '',
    external_label_id TEXT NOT NULL DEFAULT '', -- 上游标签 id（幂等键）
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_labels_external ON labels(external_label_id) WHERE external_label_id <> '';
CREATE INDEX idx_labels_name ON labels(name);

-- 交付挂标（多对多）：复合主键即幂等键——重复挂同一标签不产生重复行；
-- 两侧 CASCADE，交付/标签删除时关联随之清理。
CREATE TABLE delivery_labels (
    delivery_id UUID NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    label_id    UUID NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (delivery_id, label_id)
);
CREATE INDEX idx_delivery_labels_label ON delivery_labels(label_id);
