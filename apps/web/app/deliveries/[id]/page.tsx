"use client";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import Link from "next/link";
import { getDelivery, advanceDelivery } from "@/lib/api";
import { STAGES, GATES } from "@/lib/types";

export default function DeliveryDetailPage() {
  const params = useParams<{ id: string }>();
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["delivery", params.id],
    queryFn: () => getDelivery(params.id),
  });

  const advance = useMutation({
    mutationFn: () => advanceDelivery(params.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["delivery", params.id] }),
  });

  if (isLoading || !data) return <main className="p-8">加载中…</main>;
  const { delivery, timeline } = data;
  const currentIdx = STAGES.indexOf(delivery.current_stage as (typeof STAGES)[number]);

  return (
    <main className="max-w-3xl mx-auto p-8">
      <Link href="/" className="text-sm text-gray-500">← 返回</Link>
      <h1 className="text-2xl font-bold mt-2 mb-1">{delivery.title}</h1>
      <div className="text-sm text-gray-500 mb-6">
        {delivery.status} · 创建于 {new Date(delivery.created_at).toLocaleString()}
      </div>

      <h2 className="font-semibold mb-2">流水线</h2>
      <ol className="flex flex-wrap gap-2 mb-8">
        {STAGES.map((s, i) => {
          const done = i < currentIdx;
          const isCurrent = i === currentIdx;
          const isGate = GATES.has(s);
          return (
            <li
              key={s}
              className={[
                "px-3 py-1 rounded-full text-sm border",
                isCurrent ? "bg-black text-white" : done ? "bg-gray-100" : "text-gray-400",
                isGate ? "border-dashed" : "",
              ].join(" ")}
            >
              {s}
              {isGate ? " 🚪" : ""}
            </li>
          );
        })}
      </ol>

      {delivery.status === "active" && (
        <button
          className="bg-black text-white rounded px-4 py-2 mb-8"
          onClick={() => advance.mutate()}
        >
          推进到下一 stage →
        </button>
      )}

      <h2 className="font-semibold mb-2">时间线</h2>
      <ul className="space-y-1 text-sm">
        {timeline.map((e) => (
          <li key={e.id} className="border-l-2 pl-3 py-1">
            <span className="font-mono text-gray-500">
              {new Date(e.created_at).toLocaleTimeString()}
            </span>{" "}
            <span className="font-medium">{e.stage}</span> · {e.event_type}
          </li>
        ))}
      </ul>
    </main>
  );
}
