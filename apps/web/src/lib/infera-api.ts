/**
 * infera 后端 API 客户端（后端 :8080，dev 由 vite proxy 转发 /api）。
 */
import type {
  Agent,
  BindingMap,
  ChildSpec,
  Delivery,
  DeliveryDetail,
  GateInfo,
  PipelineInfo,
  Project,
  ProjectPipeline,
  TaskSpec,
} from './infera-types'

async function json<T>(r: Response): Promise<T> {
  if (!r.ok) {
    const e = await r.json().catch(() => ({}))
    throw new Error((e as { error?: string }).error || `HTTP ${r.status}`)
  }
  return r.json() as Promise<T>
}

// —— auth ——
export async function login(password: string): Promise<boolean> {
  const r = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  return r.ok
}
export async function logout(): Promise<boolean> {
  const r = await fetch('/api/logout', { method: 'POST' })
  return r.ok
}
export async function me(): Promise<{ logged_in: boolean }> {
  const r = await fetch('/api/me')
  if (!r.ok) return { logged_in: false }
  return r.json()
}

// —— projects ——
export async function listProjects(includeStats = false): Promise<Project[]> {
  const q = includeStats ? '?include=stats' : ''
  return json(await fetch(`/api/projects${q}`))
}
export async function createProject(input: {
  name: string
  repo_url?: string
  default_branch?: string
}): Promise<Project> {
  return json(
    await fetch('/api/projects', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
  )
}
export async function getProject(id: string): Promise<Project> {
  return json(await fetch(`/api/projects/${id}`))
}
export async function patchProjectPinned(
  id: string,
  pinned: boolean
): Promise<Project> {
  return json(
    await fetch(`/api/projects/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pinned }),
    })
  )
}
export async function listProjectDeliveries(id: string): Promise<Delivery[]> {
  return json(await fetch(`/api/projects/${id}/deliveries`))
}

// —— agent 编排 ——
export async function listAgents(): Promise<Agent[]> {
  return json(await fetch('/api/agents'))
}
export async function createAgent(input: {
  name: string
  runner: Agent['runner']
  config: Record<string, unknown>
}): Promise<Agent> {
  return json(
    await fetch('/api/agents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
  )
}
/** PATCH 局部更新；config 非 nil 即整体替换 */
export async function updateAgent(
  id: string,
  input: {
    name: string
    runner: Agent['runner']
    config: Record<string, unknown>
  }
): Promise<Agent> {
  return json(
    await fetch(`/api/agents/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
  )
}
export async function deleteAgent(id: string): Promise<void> {
  json(await fetch(`/api/agents/${id}`, { method: 'DELETE' }))
}
export async function getPipeline(): Promise<PipelineInfo> {
  return json(await fetch('/api/pipeline'))
}
/** 全量替换默认绑定（必须覆盖全部可绑定节点，否则后端 400 列缺失） */
export async function putPipeline(bindings: BindingMap): Promise<PipelineInfo> {
  return json(
    await fetch('/api/pipeline', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bindings }),
    })
  )
}
export async function getProjectPipeline(id: string): Promise<ProjectPipeline> {
  return json(await fetch(`/api/projects/${id}/pipeline`))
}
/** 全量替换项目覆盖；传 {} 清空全部覆盖、回退默认 */
export async function putProjectPipeline(
  id: string,
  bindings: BindingMap
): Promise<ProjectPipeline> {
  return json(
    await fetch(`/api/projects/${id}/pipeline`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bindings }),
    })
  )
}
export async function createDelivery(
  projectId: string,
  input: { title: string; description?: string }
): Promise<Delivery> {
  return json(
    await fetch(`/api/projects/${projectId}/deliveries`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
  )
}

// —— delivery 详情 / gate ——
export async function getDelivery(id: string): Promise<DeliveryDetail> {
  return json(await fetch(`/api/deliveries/${id}`))
}
export async function getGate(id: string): Promise<GateInfo> {
  return json(await fetch(`/api/deliveries/${id}/gate`))
}
/** 门禁批准选项（按当前门取用：spec_approval→complexity / design_approval→split / tasks_approval→tasks） */
export interface ApproveOptions {
  complexity?: 'small' | 'large'
  split?: ChildSpec[]
  tasks?: TaskSpec[]
}

export async function approveGate(
  id: string,
  opts?: ApproveOptions
): Promise<Delivery> {
  // 仅在有实质选项时携带 body；否则空 body = 普通批准
  const hasOpts =
    !!opts &&
    (!!opts.complexity || !!opts.split?.length || !!opts.tasks?.length)
  return json(
    await fetch(`/api/deliveries/${id}/approve`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: hasOpts ? JSON.stringify(opts) : undefined,
    })
  )
}
/** 合并冲突恢复：人工推送 infera/<父前8> 分支后继续父流水线 */
export async function mergeResume(id: string): Promise<Delivery> {
  return json(
    await fetch(`/api/deliveries/${id}/merge/resume`, { method: 'POST' })
  )
}
export async function rejectGate(
  id: string,
  reason: string
): Promise<Delivery> {
  return json(
    await fetch(`/api/deliveries/${id}/reject`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason }),
    })
  )
}
