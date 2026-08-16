import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'

/**
 * 订阅某 delivery 的实时事件（WebSocket）。收到任意事件就让相关 query 失效重拉，
 * 使页面自动刷新。
 */
export function useDeliveryEvents(deliveryId: string) {
  const qc = useQueryClient()
  useEffect(() => {
    const wsUrl = import.meta.env.DEV
      ? `ws://localhost:8080/ws?delivery=${deliveryId}`
      : `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws?delivery=${deliveryId}`
    const ws = new WebSocket(wsUrl)
    ws.onmessage = () => {
      qc.invalidateQueries({ queryKey: ['delivery', deliveryId] })
      qc.invalidateQueries({ queryKey: ['gate', deliveryId] })
    }
    return () => ws.close()
  }, [deliveryId, qc])
}
