-- 清除全局默认编排绑定（INFERA-180）：project_id 为空的绑定行属于已删除的
-- 全局默认机制，项目级绑定（project_id 非空）是唯一绑定来源。
-- 幂等：DELETE 无匹配行时无害，重复执行安全。
DELETE FROM pipeline_bindings WHERE project_id IS NULL;
