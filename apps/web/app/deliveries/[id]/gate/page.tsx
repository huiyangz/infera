"use client";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import { useState } from "react";
import { getGate, approveGate, rejectGate } from "@/lib/api";

export default function GatePage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["gate", params.id],
    queryFn: () => getGate(params.id),
  });
  const [reason, setReason] = useState("");

  const approve = useMutation({
    mutationFn: () => approveGate(params.id),
    onSuccess: () => {
      qc.invalidateQueries();
      router.push(`/deliveries/${params.id}`);
    },
  });
  const reject = useMutation({
    mutationFn: () => rejectGate(params.id, reason),
    onSuccess: () => {
      qc.invalidateQueries();
      router.push(`/deliveries/${params.id}`);
    },
  });

  if (isLoading || !data) return <main className="p-8">加载中…</main>;
  const isSpec = data.gate === "spec_approval";

  return (
    <main className="max-w-3xl mx-auto p-8">
      <Link href={`/deliveries/${params.id}`} className="text-sm text-gray-500">
        ← 返回详情
      </Link>
      <h1 className="text-2xl font-bold mt-2 mb-2">
        {isSpec ? "Spec 审批" : "代码审核"}
      </h1>
      <p className="text-sm text-gray-500 mb-6">gate: {data.gate}</p>

      <h2 className="font-semibold mb-2">{isSpec ? "Spec 内容" : "Reviewer 意见"}</h2>
      <pre className="bg-gray-50 border rounded p-4 whitespace-pre-wrap text-sm mb-4 min-h-24">
        {data.agent_output?.output || "（无 Agent 产出）"}
      </pre>

      {!isSpec && data.pr_url && (
        <p className="mb-4 text-sm">
          PR：
          <a
            className="text-blue-600 underline"
            href={data.pr_url}
            target="_blank"
            rel="noreferrer"
          >
            {data.pr_url}
          </a>
        </p>
      )}

      <div className="flex gap-2 items-start">
        <button
          className="bg-green-700 text-white rounded px-4 py-2"
          onClick={() => approve.mutate()}
        >
          批准 →
        </button>
        <input
          className="flex-1 border rounded px-3 py-2"
          placeholder="打回理由…"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        <button
          className="border border-red-600 text-red-700 rounded px-4 py-2"
          disabled={!reason.trim()}
          onClick={() => reject.mutate()}
        >
          打回
        </button>
      </div>
      <p className="text-xs text-gray-400 mt-2">
        批准：前进到下一 stage。打回：{isSpec ? "回 spec 重写" : "回 code_gen 重做"}。
      </p>
    </main>
  );
}
