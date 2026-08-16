/**
 * infera 后端 API 客户端（后端 :8080，dev 由 vite proxy 转发 /api）。
 */
import type { Delivery, DeliveryDetail, GateInfo, Project } from './infera-types'

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
export async function listProjects(): Promise<Project[]> {
  return json(await fetch('/api/projects'))
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
    }),
  )
}
export async function getProject(id: string): Promise<Project> {
  return json(await fetch(`/api/projects/${id}`))
}
export async function listProjectDeliveries(id: string): Promise<Delivery[]> {
  return json(await fetch(`/api/projects/${id}/deliveries`))
}
export async function createDelivery(
  projectId: string,
  input: { title: string; description?: string },
): Promise<Delivery> {
  return json(
    await fetch(`/api/projects/${projectId}/deliveries`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    }),
  )
}

// —— delivery 详情 / gate ——
export async function getDelivery(id: string): Promise<DeliveryDetail> {
  return json(await fetch(`/api/deliveries/${id}`))
}
export async function getGate(id: string): Promise<GateInfo> {
  return json(await fetch(`/api/deliveries/${id}/gate`))
}
export async function approveGate(id: string): Promise<Delivery> {
  return json(await fetch(`/api/deliveries/${id}/approve`, { method: 'POST' }))
}
export async function rejectGate(id: string, reason: string): Promise<Delivery> {
  return json(
    await fetch(`/api/deliveries/${id}/reject`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ reason }),
    }),
  )
}
