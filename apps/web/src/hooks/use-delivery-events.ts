import { useEffect, useRef } from 'react'
import { useQueryClient } from '@tanstack/react-query'

/** 断线重连退避序列（封顶 5s），成功连上后归零重置。 */
const RECONNECT_BACKOFF_MS = [500, 1000, 2000, 5000]
/**
 * 握手从未成功时的最大尝试次数：浏览器拿不到 WS 握手的 HTTP 状态，
 * 鉴权被拒（L8 服务端）与后端未启动同为 close code 1006——
 * 反复重试只会每 5s 刷一次错，达到上限即停手（曾经连上过的断线重连不受此限）。
 */
const MAX_NEVER_OPENED_ATTEMPTS = 6
/**
 * 服务器明确「拒绝」的 close code，重连不会成功，立即停手不刷屏：
 * 1008 = 策略拒绝；4401 = 约定的鉴权拒绝私有码（L8 服务端配合关闭）。
 */
const REJECTED_CLOSE_CODES = new Set([1008, 4401])
/** 事件风暴下的失效合并窗口：首条立即失效，窗口内的后续事件合并。 */
const INVALIDATE_THROTTLE_MS = 300

/**
 * 订阅某 delivery 的实时事件（WebSocket）。收到事件把相关 query 标记失效，
 * 页面自动刷新（风暴按 300ms 窗口合并，且只失效本项目相关 query）。
 * 断线按退避序列自动重连；服务器明确拒绝、正常关闭或握手持续失败则停手。
 * URL 一律同源：dev 由 vite 的 /ws 代理转发到后端（见 vite.config.ts）。
 */
export function useDeliveryEvents(deliveryId: string, projectId?: string) {
  const qc = useQueryClient()
  const socketRef = useRef<WebSocket | null>(null)
  const timerRef = useRef<number | null>(null)

  useEffect(() => {
    let disposed = false
    let attempt = 0
    let everOpened = false
    let invalidateTimer: number | null = null

    const clearTimer = () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current)
        timerRef.current = null
      }
    }

    const invalidate = () => {
      qc.invalidateQueries({ queryKey: ['delivery', deliveryId] })
      qc.invalidateQueries({ queryKey: ['gate', deliveryId] })
      // 主从布局：左侧列表与项目卡片统计也要跟着刷新（限定所属项目）
      if (projectId) {
        qc.invalidateQueries({ queryKey: ['project', projectId] })
        qc.invalidateQueries({ queryKey: ['project-deliveries', projectId] })
      } else {
        qc.invalidateQueries({ queryKey: ['projects'] })
        qc.invalidateQueries({ queryKey: ['project-deliveries'] })
      }
    }
    // 首条事件立即失效，窗口内的突发合并为一次，别逐条全量失效打爆后端
    const throttledInvalidate = () => {
      if (invalidateTimer !== null) return
      invalidate()
      invalidateTimer = window.setTimeout(() => {
        invalidateTimer = null
      }, INVALIDATE_THROTTLE_MS)
    }

    const connect = () => {
      if (disposed) return
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      const ws = new WebSocket(
        `${proto}://${location.host}/ws?delivery=${deliveryId}`
      )
      socketRef.current = ws
      ws.onopen = () => {
        everOpened = true
        attempt = 0
      }
      ws.onmessage = throttledInvalidate
      ws.onclose = (ev) => {
        if (disposed) return
        // 鉴权/策略被拒：重连无意义，停手
        if (REJECTED_CLOSE_CODES.has(ev.code)) return
        // 正常关闭（服务器主动收摊）：不重连
        if (ev.code === 1000) return
        // 从未连上过却一直失败：握手被拒或后端不在，别无限刷
        if (!everOpened && attempt >= MAX_NEVER_OPENED_ATTEMPTS) return
        clearTimer()
        const delay =
          RECONNECT_BACKOFF_MS[
            Math.min(attempt, RECONNECT_BACKOFF_MS.length - 1)
          ]
        attempt += 1
        timerRef.current = window.setTimeout(connect, delay)
      }
    }

    connect()

    return () => {
      disposed = true
      clearTimer()
      if (invalidateTimer !== null) window.clearTimeout(invalidateTimer)
      socketRef.current?.close()
      socketRef.current = null
    }
  }, [deliveryId, projectId, qc])
}
