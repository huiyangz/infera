import type { Delivery, DeliveryLabel } from '@/lib/infera-types'

/**
 * 需求发现面的类型（契约冻结于 INFERA-225：
 * server/internal/api/discovery.go 的 discoveryTaskRow，JSON 标签即契约）。
 */

/** agent 类型词表：mining=需求挖掘、analysis=需求分析 */
export type DiscoveryAgentType = 'mining' | 'analysis'

/**
 * 需求发现列表行：store.Delivery 全字段平铺（与 task-groups 顶层行同款）
 * + 3 个视图装配字段。updated_at 降序。
 */
export interface DiscoveryTaskRow extends Delivery {
  /** 该卡命中的 agent 类型全集（双标签卡两类都报；分组/筛选用全集而非仅命中项） */
  agent_types: DiscoveryAgentType[]
  /** 跨项目展示名（JOIN 语义，装配期带出） */
  project_name: string
  /** 挂的标签（契约恒为数组，未挂 = 空数组） */
  labels: DeliveryLabel[]
}
