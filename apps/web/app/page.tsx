"use client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { listProjects } from "@/lib/api";
import { useRequireAuth } from "@/lib/useRequireAuth";

export default function Home() {
  const { loggedIn, loading } = useRequireAuth();
  const { data } = useQuery({ queryKey: ["projects"], queryFn: listProjects, enabled: loggedIn });

  if (loading || !loggedIn) return <main className="p-8">加载中…</main>;
  return (
    <main className="max-w-4xl mx-auto p-8">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">项目</h1>
        <Link href="/projects/new" className="rounded px-4 py-2 text-white" style={{ background: "var(--accent)" }}>
          新建项目
        </Link>
      </div>
      {data?.length === 0 && <p style={{ color: "var(--muted)" }}>还没有项目。</p>}
      <div className="grid gap-3">
        {data?.map((p) => (
          <Link
            key={p.id} href={`/projects/${p.id}`}
            className="border rounded p-4 flex justify-between items-center hover:opacity-80"
            style={{ borderColor: "var(--border)", background: "var(--card)" }}
          >
            <div>
              <div className="font-medium">{p.name}</div>
              <div className="text-sm mono" style={{ color: "var(--muted)" }}>
                {p.repo_url || "（未绑仓库）"} · {p.default_branch}
              </div>
            </div>
          </Link>
        ))}
      </div>
    </main>
  );
}
