import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'

/** 断线重连退避序列（封顶 5s），成功连上后归零重置。 */
const RECONNECT_BACKOFF_MS = [500, 1000, 2000, 5000]

/**
 * 订阅某 delivery 的实时事件（WebSocket）。收到任意事件就让相关 query 失效重拉，
 * 使页面自动刷新。断线后按退避序列自动重连。
 */
export function useDeliveryEvents(deliveryId: string) {
  const qc = useQueryClient()
  const socketRef = useRef<WebSocket | null>(null)
  const timerRef = useRef<number | null>(null)

  useEffect(() => {
    let disposed = false
    let attempt = 0

    const clearTimer = () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }

    const connect = () => {
      if (disposed) return
      const wsUrl = import.meta.env.DEV
        ? `ws://localhost:8080/ws?delivery=${deliveryId}`
        : `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws?delivery=${deliveryId}`
      const ws = new WebSocket(wsUrl)
      socketRef.current = ws
      ws.onopen = () => {
        attempt = 0
      }
      ws.onmessage = () => {
        qc.invalidateQueries({ queryKey: ['delivery', deliveryId] })
        qc.invalidateQueries({ queryKey: ['gate', deliveryId] })
      }
      // onerror 与随后的 onclose 都可能触发：先清旧定时器，保证同时只有一个重连在排队。
      const scheduleReconnect = () => {
        if (disposed) return
        clearTimer()
        const delay =
          RECONNECT_BACKOFF_MS[
            Math.min(attempt, RECONNECT_BACKOFF_MS.length - 1)
          ]
        attempt += 1
        timerRef.current = window.setTimeout(connect, delay)
      }
      ws.onclose = scheduleReconnect
      ws.onerror = scheduleReconnect
    }

    connect()

    return () => {
      disposed = true
      clearTimer()
      socketRef.current?.close()
      socketRef.current = null
    }
  }, [deliveryId, qc])
}
