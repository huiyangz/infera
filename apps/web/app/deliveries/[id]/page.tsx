"use client";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import Link from "next/link";
import { getDelivery } from "@/lib/api";
import { useDeliveryEvents } from "@/lib/useDeliveryEvents";
import { STAGES, GATES } from "@/lib/types";

export default function DeliveryDetailPage() {
  const params = useParams<{ id: string }>();
  useDeliveryEvents(params.id);
  const { data, isLoading } = useQuery({
    queryKey: ["delivery", params.id],
    queryFn: () => getDelivery(params.id),
  });

  if (isLoading || !data) return <main className="p-8">加载中…</main>;
  const { delivery, timeline } = data;
  const currentIdx = STAGES.indexOf(delivery.current_stage as (typeof STAGES)[number]);

  return (
    <main className="max-w-3xl mx-auto p-8">
      <Link href={`/projects/${delivery.project_id}`} className="text-sm" style={{ color: "var(--muted)" }}>
        ← 项目
      </Link>
      <h1 className="text-2xl font-bold mt-2 mb-1">{delivery.title}</h1>
      <div className="text-sm mb-6" style={{ color: "var(--muted)" }}>
        {delivery.status} · 创建于 {new Date(delivery.created_at).toLocaleString()}
      </div>

      {delivery.pending_gate && (
        <Link
          href={`/deliveries/${params.id}/gate`}
          className="inline-block rounded px-4 py-2 mb-4 text-white"
          style={{ background: "var(--warn)" }}
        >
          需审批：{delivery.pending_gate} → 去审批
        </Link>
      )}

      <h2 className="font-semibold mb-2">流水线</h2>
      <ol className="flex flex-wrap gap-2 mb-8">
        {STAGES.map((s, i) => {
          const done = i < currentIdx;
          const isCurrent = i === currentIdx;
          const isGate = GATES.has(s);
          const style: React.CSSProperties = isCurrent
            ? { background: "var(--accent)", color: "#fff" }
            : done
              ? { background: "var(--card)" }
              : { color: "var(--muted)" };
          return (
            <li
              key={s}
              className={`px-3 py-1 rounded-full text-sm border ${isGate ? "border-dashed" : ""}`}
              style={style}
            >
              {s}
              {isGate ? " 🚪" : ""}
            </li>
          );
        })}
      </ol>

      <h2 className="font-semibold mb-2">时间线</h2>
      <ul className="space-y-1 text-sm">
        {timeline.map((e) => (
          <li key={e.id} className="border-l-2 pl-3 py-1" style={{ borderColor: "var(--border)" }}>
            <span className="font-mono" style={{ color: "var(--muted)" }}>
              {new Date(e.created_at).toLocaleTimeString()}
            </span>{" "}
            <span className="font-medium">{e.stage}</span> · {e.event_type}
          </li>
        ))}
      </ul>
    </main>
  );
}
