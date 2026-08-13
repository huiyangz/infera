"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient, useMutation } from "@tanstack/react-query";
import { createProject } from "@/lib/api";
import { useRequireAuth } from "@/lib/useRequireAuth";

export default function NewProjectPage() {
  const { loggedIn, loading } = useRequireAuth();
  const router = useRouter();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [repo, setRepo] = useState("");
  const [branch, setBranch] = useState("main");
  const [err, setErr] = useState("");
  const m = useMutation({
    mutationFn: () => createProject({ name, repo_url: repo, default_branch: branch }),
    onSuccess: (p) => { qc.invalidateQueries({ queryKey: ["projects"] }); router.replace(`/projects/${p.id}`); },
    onError: (e: Error) => setErr(e.message),
  });
  if (loading || !loggedIn) return <main className="p-8">加载中…</main>;
  return (
    <main className="max-w-lg mx-auto p-8">
      <h1 className="text-2xl font-bold mb-6">新建项目</h1>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (name.trim()) m.mutate(); }}>
        <label className="block">
          <span className="text-sm">项目名</span>
          <input className="w-full border rounded px-3 py-2 mt-1" style={{ borderColor: "var(--border)", background: "var(--card)" }}
            value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="block">
          <span className="text-sm">Git 仓库（绑一次；留空=绿地新项目）</span>
          <input className="w-full border rounded px-3 py-2 mt-1 mono" style={{ borderColor: "var(--border)", background: "var(--card)" }}
            placeholder="https://github.com/you/repo.git" value={repo} onChange={(e) => setRepo(e.target.value)} />
        </label>
        <label className="block">
          <span className="text-sm">默认分支</span>
          <input className="w-full border rounded px-3 py-2 mt-1" style={{ borderColor: "var(--border)", background: "var(--card)" }}
            value={branch} onChange={(e) => setBranch(e.target.value)} />
        </label>
        {err && <p className="text-sm" style={{ color: "var(--bad)" }}>{err}</p>}
        <button className="rounded px-4 py-2 text-white" style={{ background: "var(--accent)" }}>创建并绑定</button>
        <p className="text-xs" style={{ color: "var(--muted)" }}>填了仓库会先试 clone 校验可达 + 有写权限。</p>
      </form>
    </main>
  );
}
