"use client";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import Link from "next/link";
import { useState } from "react";
import { getProject, listProjectDeliveries, createDelivery } from "@/lib/api";
import { useRequireAuth } from "@/lib/useRequireAuth";

export default function ProjectPage() {
  const params = useParams<{ id: string }>();
  const { loggedIn, loading } = useRequireAuth();
  const qc = useQueryClient();
  const { data: proj } = useQuery({ queryKey: ["project", params.id], queryFn: () => getProject(params.id), enabled: loggedIn });
  const { data: items } = useQuery({ queryKey: ["project-deliveries", params.id], queryFn: () => listProjectDeliveries(params.id), enabled: loggedIn });
  const [title, setTitle] = useState("");
  const create = useMutation({
    mutationFn: () => createDelivery(params.id, { title }),
    onSuccess: () => { setTitle(""); qc.invalidateQueries({ queryKey: ["project-deliveries", params.id] }); },
  });

  if (loading || !loggedIn || !proj) return <main className="p-8">加载中…</main>;
  return (
    <main className="max-w-4xl mx-auto p-8">
      <Link href="/" className="text-sm" style={{ color: "var(--muted)" }}>← 项目</Link>
      <h1 className="text-2xl font-bold mt-2 mb-1">{proj.name}</h1>
      <div className="text-sm mono mb-6" style={{ color: "var(--muted)" }}>
        {proj.repo_url || "（未绑仓库）"} · {proj.default_branch}
      </div>

      <form className="flex gap-2 mb-6" onSubmit={(e) => { e.preventDefault(); if (title.trim()) create.mutate(); }}>
        <input className="flex-1 border rounded px-3 py-2" style={{ borderColor: "var(--border)", background: "var(--card)" }}
          placeholder="一句话需求…" value={title} onChange={(e) => setTitle(e.target.value)} />
        <button className="rounded px-4 py-2 text-white" style={{ background: "var(--accent)" }}>新建交付</button>
      </form>

      <ul className="space-y-2">
        {items?.map((d) => (
          <li key={d.id} className="border rounded p-4 flex justify-between items-center"
              style={{ borderColor: "var(--border)", background: "var(--card)" }}>
            <div>
              <Link href={`/deliveries/${d.id}`} className="font-medium hover:underline">{d.title}</Link>
              <div className="text-sm flex gap-2 items-center" style={{ color: "var(--muted)" }}>
                <span>{d.current_stage}</span>
                {d.status === "blocked" && <span className="px-2 py-0.5 rounded text-xs" style={{ background: "var(--bad)", color: "#fff" }}>已升级</span>}
                {d.pending_gate && <Link href={`/deliveries/${d.id}/gate`} className="px-2 py-0.5 rounded text-xs" style={{ background: "var(--warn)", color: "#fff" }}>待审批</Link>}
              </div>
            </div>
            <span className="text-xs mono" style={{ color: "var(--muted)" }}>{d.status}</span>
          </li>
        ))}
      </ul>
    </main>
  );
}
