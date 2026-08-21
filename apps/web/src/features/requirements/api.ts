/**
 * 需求流转 API client（契约冻结于 T05：server/internal/api/requirements.go）。
 * 错误形态沿用 lib/infera-api 的 ApiError（status + 后端 error 文案）。
 */
import { ApiError } from '@/lib/infera-api'
import type {
  AuditEntry,
  CreateRequirementInput,
  MergePolicy,
  MergeResult,
  Requirement,
  RequirementDetail,
  RequirementListItem,
} from './types'

async function json<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const e = await r.json().catch(() => ({}))
    throw new ApiError(
      r.status,
      (e as { error?: string }).error || `HTTP ${r.status}`
    )
  }
  return r.json() as Promise<T>
}

async function post<T>(url: string, body?: unknown): Promise<T> {
  return json<T>(
    await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  )
}

// —— 需求行 ——

export async function createRequirement(
  input: CreateRequirementInput
): Promise<Requirement> {
  return post<Requirement>('/api/requirements', input)
}

export async function listRequirements(): Promise<RequirementListItem[]> {
  return json(await fetch('/api/requirements'))
}

export async function getRequirement(id: string): Promise<RequirementDetail> {
  return json(await fetch(`/api/requirements/${id}`))
}

// —— 卡片代理动作（评论类动作成功响应 {ok:true}，前端只关心不抛错） ——

export function approveCard(
  requirementId: string,
  cardId: string
): Promise<{ ok: boolean }> {
  return post(`/api/requirements/${requirementId}/cards/${cardId}/approve`)
}

export function rejectCard(
  requirementId: string,
  cardId: string,
  feedback: string
): Promise<{ ok: boolean }> {
  return post(`/api/requirements/${requirementId}/cards/${cardId}/reject`, {
    feedback,
  })
}

export function decideCard(
  requirementId: string,
  cardId: string,
  choice: string,
  text: string
): Promise<{ ok: boolean }> {
  return post(`/api/requirements/${requirementId}/cards/${cardId}/decide`, {
    choice,
    text,
  })
}

/** 终审合并：响应携带 github.MergeResult（merged/sha/message） */
export function mergeCard(
  requirementId: string,
  cardId: string
): Promise<MergeResult> {
  return post<MergeResult>(
    `/api/requirements/${requirementId}/cards/${cardId}/merge`
  )
}

export function reworkCard(
  requirementId: string,
  cardId: string,
  feedback: string
): Promise<{ ok: boolean }> {
  return post(`/api/requirements/${requirementId}/cards/${cardId}/rework`, {
    feedback,
  })
}

// —— 审计 ——

export async function listRequirementAudit(
  requirementId: string
): Promise<AuditEntry[]> {
  return json(await fetch(`/api/requirements/${requirementId}/audit`))
}

// —— 项目合并策略（FR-6） ——

export async function getMergePolicy(projectId: string): Promise<MergePolicy> {
  return json(await fetch(`/api/projects/${projectId}/merge-policy`))
}

export async function setMergePolicy(
  projectId: string,
  policy: MergePolicy
): Promise<MergePolicy> {
  return json(
    await fetch(`/api/projects/${projectId}/merge-policy`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(policy),
    })
  )
}
