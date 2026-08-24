/**
 * 需求发现页的纯筛选/分组逻辑（INFERA-226）：行内已带 status 与
 * agent_types 全集，页内筛选与分组全部客户端完成（契约层有意不加
 * status 参数）。纯函数、无 IO，供页面与测试直接消费。
 */
import type { DeliveryStatus } from '@/lib/infera-types'
import type { DiscoveryAgentType, DiscoveryTaskRow } from './types'

/** agent 筛选值：all = 两类并集 */
export type AgentFilter = 'all' | DiscoveryAgentType

/** 状态筛选值：all = 全部状态 */
export type StatusFilter = 'all' | DeliveryStatus

/** 分组模式：none = 平铺不分组 */
export type GroupMode = 'none' | 'agent' | 'status'

/** agent 类型中文标签（与上游两类 agent 的语义命名一致） */
export const AGENT_LABEL: Record<DiscoveryAgentType, string> = {
  mining: '需求挖掘',
  analysis: '需求分析',
}

/** agent 固定序（对齐后端 discoveryAgentTypes：mining → analysis） */
export const AGENT_ORDER: readonly DiscoveryAgentType[] = ['mining', 'analysis']

/** 状态中文标签（与 StatusBadge / 主看板任务卡文案同口径） */
export const STATUS_LABEL: Record<DeliveryStatus, string> = {
  active: '进行中',
  blocked: '已阻塞',
  queued: '未启动',
  completed: '已完成',
}

/** 状态分组固定序：活跃在前，已完成殿后 */
export const STATUS_ORDER: readonly DeliveryStatus[] = [
  'active',
  'blocked',
  'queued',
  'completed',
]

/**
 * 页内筛选：agent 按 agent_types 全集命中（双标签卡在任一类型筛选下都
 * 保留），status 精确匹配，二者叠加取交集。不改变行序（沿用后端
 * updated_at 降序）。
 */
export function filterDiscoveryTasks(
  rows: DiscoveryTaskRow[],
  agent: AgentFilter,
  status: StatusFilter
): DiscoveryTaskRow[] {
  return rows.filter(
    (r) =>
      (agent === 'all' || r.agent_types.includes(agent)) &&
      (status === 'all' || r.status === status)
  )
}

/** 一个分组：key 为分组维度取值（none 模式恒 'all'），label 空串 = 无组头 */
export interface TaskGroup {
  key: string
  label: string
  rows: DiscoveryTaskRow[]
}

/**
 * 页内分组：agent 模式按类型全集分组（双标签卡在两组都出现，组序
 * mining → analysis）；status 模式按状态分组（组序 STATUS_ORDER）；
 * none 模式单一无标签组原样承载。空组不产出。
 */
export function groupDiscoveryTasks(
  rows: DiscoveryTaskRow[],
  mode: GroupMode
): TaskGroup[] {
  if (mode === 'none') return [{ key: 'all', label: '', rows }]
  if (mode === 'agent') {
    return AGENT_ORDER.flatMap((t) => {
      const hit = rows.filter((r) => r.agent_types.includes(t))
      return hit.length ? [{ key: t, label: AGENT_LABEL[t], rows: hit }] : []
    })
  }
  return STATUS_ORDER.flatMap((s) => {
    const hit = rows.filter((r) => r.status === s)
    return hit.length ? [{ key: s, label: STATUS_LABEL[s], rows: hit }] : []
  })
}
