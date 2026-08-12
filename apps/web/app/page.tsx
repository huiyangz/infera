"use client";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { listDeliveries, createDelivery, advanceDelivery } from "@/lib/api";

export default function Home() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["deliveries"],
    queryFn: listDeliveries,
  });

  const [title, setTitle] = useState("");
  const create = useMutation({
    mutationFn: () => createDelivery({ title }),
    onSuccess: () => {
      setTitle("");
      qc.invalidateQueries({ queryKey: ["deliveries"] });
    },
  });

  const advance = useMutation({
    mutationFn: (id: string) => advanceDelivery(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deliveries"] }),
  });

  return (
    <main className="max-w-3xl mx-auto p-8">
      <h1 className="text-2xl font-bold mb-6">infera · Deliveries</h1>

      <form
        className="flex gap-2 mb-8"
        onSubmit={(e) => {
          e.preventDefault();
          if (title.trim()) create.mutate();
        }}
      >
        <input
          className="flex-1 border rounded px-3 py-2"
          placeholder="一句话需求…"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
        <button className="bg-black text-white rounded px-4 py-2" type="submit">
          创建
        </button>
      </form>

      {isLoading ? (
        <p>加载中…</p>
      ) : (
        <ul className="space-y-2">
          {data?.map((d) => (
            <li key={d.id} className="border rounded p-4 flex items-center justify-between">
              <div>
                <Link href={`/deliveries/${d.id}`} className="font-medium hover:underline">
                  {d.title}
                </Link>
                <div className="text-sm text-gray-500 flex gap-2 items-center">
                  <span>{d.current_stage}</span>
                  {d.status === "blocked" && (
                    <span className="bg-red-100 text-red-700 px-2 py-0.5 rounded text-xs">
                      已升级 · 需人工介入
                    </span>
                  )}
                  {d.pending_gate && (
                    <Link
                      href={`/deliveries/${d.id}/gate`}
                      className="bg-yellow-100 text-yellow-800 px-2 py-0.5 rounded text-xs"
                    >
                      待审批：{d.pending_gate}
                    </Link>
                  )}
                  {!d.pending_gate && d.status === "active" && <span>· {d.status}</span>}
                </div>
              </div>
              <button
                className="text-sm border rounded px-3 py-1"
                onClick={() => advance.mutate(d.id)}
              >
                推进 →
              </button>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
