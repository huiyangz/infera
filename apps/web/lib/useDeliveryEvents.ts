import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

/**
 * 订阅某 delivery 的实时事件（P6 WebSocket）。收到任意事件就让相关 query 失效重拉，
 * 使详情页 timeline 自动刷新。直连后端 :8080（后端 upgrader 放开跨域）。
 */
export function useDeliveryEvents(deliveryId: string) {
  const qc = useQueryClient();
  useEffect(() => {
    const ws = new WebSocket(`ws://localhost:8080/ws?delivery=${deliveryId}`);
    ws.onmessage = () => {
      qc.invalidateQueries({ queryKey: ["delivery", deliveryId] });
      qc.invalidateQueries({ queryKey: ["gate", deliveryId] });
    };
    return () => ws.close();
  }, [deliveryId, qc]);
}
