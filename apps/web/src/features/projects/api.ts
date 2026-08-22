/**
 * 需求创建 API client（契约冻结于 L202608230412-1-T01：
 * server/internal/api/requirementcreate.go）：POST /api/projects/{id}/requirements
 * 上游建卡 + 同步回流，201 返回同步侧 Delivery 行。
 * 错误形态沿用 lib/infera-api 的 ApiError（status + 后端 error 文案）。
 */
import { ApiError } from '@/lib/infera-api'
import type { Delivery } from '@/lib/infera-types'

/** 创建载荷面（冻结 DTO：title / description / status / priority / auto_merge / agent_id） */
export interface CreateProjectRequirementInput {
  title: string
  description?: string
  /** backlog 待规划（缺省，不触发 agent run）/ todo 待办（指派即唤醒） */
  status?: 'backlog' | 'todo'
  /** 上游词表透传：urgent/high/medium/low/none */
  priority?: string
  /** true → 上游 auto label；缺省关 */
  auto_merge?: boolean
  /** 空/缺省 = Tech Lead（服务端解析） */
  agent_id?: string
}

async function json<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const e = await r.json().catch(() => ({}))
    throw new ApiError(
      r.status,
      (e as { error?: string }).error || `HTTP ${r.status}`,
    )
  }
  return r.json() as Promise<T>
}

/** 在项目下创建需求；成功响应即同步回流后的 Delivery 行 */
export async function createProjectRequirement(
  projectId: string,
  input: CreateProjectRequirementInput,
): Promise<Delivery> {
  return json<Delivery>(
    await fetch(`/api/projects/${projectId}/requirements`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    }),
  )
}
