"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { login } from "@/lib/api";

export default function LoginPage() {
  const router = useRouter();
  const qc = useQueryClient();
  const [pw, setPw] = useState("");
  const [err, setErr] = useState("");
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    const ok = await login(pw);
    if (!ok) { setErr("密码错误"); return; }
    qc.invalidateQueries({ queryKey: ["me"] });
    router.replace("/");
  };
  return (
    <main className="max-w-sm mx-auto p-10">
      <h1 className="text-xl font-bold mb-6">infera 登录</h1>
      <form onSubmit={submit} className="space-y-3">
        <input
          type="password" className="w-full border rounded px-3 py-2"
          style={{ borderColor: "var(--border)", background: "var(--card)" }}
          placeholder="密码" value={pw} onChange={(e) => setPw(e.target.value)}
        />
        {err && <p style={{ color: "var(--bad)" }} className="text-sm">{err}</p>}
        <button className="w-full rounded px-4 py-2 text-white" style={{ background: "var(--accent)" }}>
          登录
        </button>
      </form>
    </main>
  );
}
