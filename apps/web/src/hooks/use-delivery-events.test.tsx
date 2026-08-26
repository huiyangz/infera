import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { useDeliveryEvents } from './use-delivery-events'

/**
 * WebSocket 替身：仅覆盖 hook 用到的表面（构造、onopen/onmessage/onclose、
 * close）。hook 只赋值回调与调 close，不发数据、不读 readyState。
 */
class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  url: string
  onopen: (() => void) | null = null
  onmessage: ((ev: unknown) => void) | null = null
  onclose: ((ev: { code: number }) => void) | null = null
  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }
  close() {
    // 正常关闭码：hook 侧不重连，测试无残留定时器
    this.onclose?.({ code: 1000 })
  }
}

function Harness({ deliveryId }: { deliveryId: string }) {
  useDeliveryEvents(deliveryId, 'p1')
  return null
}

beforeEach(() => {
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket)
})

afterEach(async () => {
  await cleanup()
  vi.unstubAllGlobals()
})

describe('useDeliveryEvents 失效面（INFERA-297：进度区随实时事件刷新）', () => {
  it('WS 事件把 delivery-progress query 一并失效（进度区与真实执行对齐）', async () => {
    const qc = new QueryClient()
    const spy = vi.spyOn(qc, 'invalidateQueries')
    await render(
      <QueryClientProvider client={qc}>
        <Harness deliveryId='d1' />
      </QueryClientProvider>
    )
    expect(FakeWebSocket.instances.length).toBe(1)

    FakeWebSocket.instances[0]!.onmessage?.({ data: '{}' })

    await vi.waitFor(() => {
      expect(spy).toHaveBeenCalledWith({
        queryKey: ['delivery-progress', 'd1'],
      })
    })
  })
})
