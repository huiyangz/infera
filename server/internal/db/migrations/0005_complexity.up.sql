-- 需求复杂度：spec_approval 门裁定。''（老数据）按 small 走。
ALTER TABLE deliveries ADD COLUMN complexity TEXT NOT NULL DEFAULT '';
