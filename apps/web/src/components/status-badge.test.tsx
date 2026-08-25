import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render } from 'vitest-browser-react'
import { StatusBadge } from './status-badge'

afterEach(async () => {
  await cleanup()
})

describe('StatusBadge 状态徽标（INFERA-233：cancelled 全站接入）', () => {
  it('cancelled 渲染「已取消」', async () => {
    const screen = await render(<StatusBadge status='cancelled' />)
    await expect
      .element(screen.getByText('已取消', { exact: true }))
      .toBeInTheDocument()
  })

  it('cancelled 与既有四态文案互不混淆（五态各得其所）', async () => {
    const labels: Array<[string, string]> = [
      ['active', '进行中'],
      ['queued', '未启动'],
      ['blocked', '已阻塞'],
      ['completed', '已完成'],
      ['cancelled', '已取消'],
    ]
    for (const [status, label] of labels) {
      const screen = await render(<StatusBadge status={status} />)
      await expect
        .element(screen.getByText(label, { exact: true }))
        .toBeInTheDocument()
      await screen.unmount()
    }
  })

  it('cancelled 徽标走中性灰系（描边 + 弱化文字 + 灰点），区别于已完成的灰底填充', async () => {
    const screen = await render(<StatusBadge status='cancelled' />)
    // getByText 命中的即徽标 span 本身（文本是它的直接子节点）
    const badge = (await screen.getByText('已取消', { exact: true }).element())!
    // 中性弱化：描边徽标 + muted 文字（放弃 = 退出主视线，不抢层级），
    // 不是已完成的 secondary 灰底填充，也不是阻塞的墨底白字
    expect(badge.className).toContain('text-muted-foreground')
    expect(badge.className).not.toContain('bg-secondary')
    expect(badge.className).not.toContain('bg-primary')
    // 与未启动（同为描边）靠灰点区分
    expect(badge.querySelector('span')?.className).toContain(
      'bg-foreground/40'
    )
  })
})
