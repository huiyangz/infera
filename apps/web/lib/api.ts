import type { Delivery, DeliveryDetail } from "./types";

export async function listDeliveries(): Promise<Delivery[]> {
  const r = await fetch("/api/deliveries");
  if (!r.ok) throw new Error("list failed");
  return r.json();
}

export async function createDelivery(input: {
  title: string;
  description?: string;
  repo_url?: string;
}): Promise<Delivery> {
  const r = await fetch("/api/deliveries", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!r.ok) throw new Error("create failed");
  return r.json();
}

export async function getDelivery(id: string): Promise<DeliveryDetail> {
  const r = await fetch(`/api/deliveries/${id}`);
  if (!r.ok) throw new Error("get failed");
  return r.json();
}

export async function advanceDelivery(id: string): Promise<Delivery> {
  const r = await fetch(`/api/deliveries/${id}/advance`, { method: "POST" });
  if (!r.ok) throw new Error("advance failed");
  return r.json();
}
