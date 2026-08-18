-- 拆分子需求查询（ListChildDeliveries / 合并循环）走 parent_id；
-- FK 列无索引时每次合并循环全表扫描。
CREATE INDEX IF NOT EXISTS idx_deliveries_parent ON deliveries(parent_id);
