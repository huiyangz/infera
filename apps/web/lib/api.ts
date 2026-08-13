import type { Delivery, DeliveryDetail, Project } from "./types";

// —— auth ——
export async function login(password: string): Promise<boolean> {
  const r = await fetch("/api/login", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
  });
  return r.ok;
}
export async function logout(): Promise<boolean> {
  const r = await fetch("/api/logout", { method: "POST" });
  return r.ok;
}
export async function me(): Promise<{ logged_in: boolean }> {
  const r = await fetch("/api/me");
  if (!r.ok) return { logged_in: false };
  return r.json();
}

// —— projects ——
export async function listProjects(): Promise<Project[]> {
  const r = await fetch("/api/projects"); if (!r.ok) throw new Error("list projects"); return r.json();
}
export async function createProject(input: { name: string; repo_url?: string; default_branch?: string }): Promise<Project> {
  const r = await fetch("/api/projects", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(input),
  });
  if (!r.ok) { const e = await r.json().catch(() => ({})); throw new Error(e.error || "create project"); }
  return r.json();
}
export async function getProject(id: string): Promise<Project> {
  const r = await fetch(`/api/projects/${id}`); if (!r.ok) throw new Error("get project"); return r.json();
}
export async function listProjectDeliveries(id: string): Promise<Delivery[]> {
  const r = await fetch(`/api/projects/${id}/deliveries`); if (!r.ok) throw new Error("list deliveries"); return r.json();
}
export async function createDelivery(projectId: string, input: { title: string; description?: string }): Promise<Delivery> {
  const r = await fetch(`/api/projects/${projectId}/deliveries`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(input),
  });
  if (!r.ok) throw new Error("create delivery"); return r.json();
}

// —— delivery 详情 / gate ——
export async function getDelivery(id: string): Promise<DeliveryDetail> {
  const r = await fetch(`/api/deliveries/${id}`); if (!r.ok) throw new Error("get delivery"); return r.json();
}
export async function getGate(id: string): Promise<GateInfo> {
  const r = await fetch(`/api/deliveries/${id}/gate`); if (!r.ok) throw new Error("gate"); return r.json();
}
export async function approveGate(id: string): Promise<Delivery> {
  const r = await fetch(`/api/deliveries/${id}/approve`, { method: "POST" }); if (!r.ok) throw new Error("approve"); return r.json();
}
export async function rejectGate(id: string, reason: string): Promise<Delivery> {
  const r = await fetch(`/api/deliveries/${id}/reject`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ reason }),
  }); if (!r.ok) throw new Error("reject"); return r.json();
}
export interface GateInfo {
  delivery_id: string; gate: string;
  agent_output: { agent?: string; output?: string } | null; pr_url: string;
}
