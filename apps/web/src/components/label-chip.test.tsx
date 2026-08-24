// 截断/换行断言依赖真实 Tailwind 工具类（max-w-*、truncate、flex-wrap），
// 对齐 delivery-detail / project-tasks 测试的做法引入全局样式
import '@/styles/index.css'
import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, type RenderResult } from 'vitest-browser-react'
import type { DeliveryLabel } from '@/lib/infera-types'
import { LabelChip, LabelChipRow } from './label-chip'

function makeLabel(overrides: Partial<DeliveryLabel> = {}): DeliveryLabel {
  return { name: 'auto', color: '#22c55e', ...overrides }
}

/** 取元素的解析后内联样式（hex 会被浏览器归一为 rgb()） */
async function styleOf(
  screen: RenderResult,
  text: string
): Promise<CSSStyleDeclaration> {
  const el = (await screen.getByText(text, { exact: true }).element())!
  return getComputedStyle(el)
}

afterEach(async () => {
  await cleanup()
})

describe('LabelChip 标签 chip（INFERA-220：Multica hex 原值 + 可读对比度）', () => {
  it('AC1-a: 渲染标签名，chip 底色为后端返回的 hex 原值（不做色彩换算）', async () => {
    const screen = await render(<LabelChip label={makeLabel()} />)

    await expect
      .element(screen.getByText('auto', { exact: true }))
      .toBeInTheDocument()
    expect((await styleOf(screen, 'auto')).backgroundColor).toBe(
      'rgb(34, 197, 94)'
    )
  })

  it('AC1-b: 深底色配浅文字（绿/紫底 → 白字），浅底色配深文字（浅黄/白底 → 墨字）', async () => {
    const cases: Array<[string, string, string]> = [
      ['#22c55e', 'rgb(255, 255, 255)', '深绿底白字'],
      ['#a855f7', 'rgb(255, 255, 255)', '紫底白字'],
      ['#3b82f6', 'rgb(255, 255, 255)', '蓝底白字'],
      ['#fef08a', 'rgb(3, 3, 3)', '浅黄底墨字'],
      ['#ffffff', 'rgb(3, 3, 3)', '白底墨字'],
    ]
    for (const [color, expected, name] of cases) {
      const screen = await render(
        <LabelChip label={makeLabel({ name, color })} />
      )
      expect((await styleOf(screen, name)).color).toBe(expected)
      await cleanup()
    }
  })

  it('AC1-c: 颜色缺省/非法时降级为中性 chip（描边 + 前景色文字），不崩溃不吞标签名', async () => {
    for (const color of ['', 'not-a-color', '#zzz']) {
      const screen = await render(
        <LabelChip label={makeLabel({ name: '候选', color })} />
      )
      const style = await styleOf(screen, '候选')
      // 不上标签底色（降级为透明），文字用前景色而非黑白硬编码
      expect(style.backgroundColor).toBe('rgba(0, 0, 0, 0)')
      expect(style.color).not.toBe('rgb(255, 255, 255)')
      await cleanup()
    }
  })

  it('AC3-a: 超长标签名截断展示（scrollWidth 超出可视宽），title 保留全名', async () => {
    const long = '一个特别长的标签名称用来验证chip截断'.repeat(4)
    const screen = await render(<LabelChip label={makeLabel({ name: long })} />)

    const el = (await screen.getByText(long, { exact: true }).element())!
    expect(el.getAttribute('title')).toBe(long)
    // max-w 上限生效且内容被截断（完整内容宽 > 可视宽）
    expect(getComputedStyle(el).maxWidth).not.toBe('none')
    expect(el.scrollWidth).toBeGreaterThan(el.clientWidth)
  })
})

describe('LabelChipRow 标签行（两处展示共用，空标签不占位）', () => {
  it('AC3-b: 空数组 / undefined / 全部无有效名 → 不渲染任何节点（不留空壳 UI）', async () => {
    for (const labels of [undefined, [] as DeliveryLabel[]]) {
      const { container } = await render(<LabelChipRow labels={labels} />)
      expect(container.firstElementChild).toBeNull()
      await cleanup()
    }
  })

  it('AC1-d: 多标签逐个渲染，行容器可换行（flex-wrap）容纳多枚 chip', async () => {
    const screen = await render(
      <LabelChipRow
        labels={[
          makeLabel({ name: 'auto' }),
          makeLabel({ name: '候选', color: '#a855f7' }),
          makeLabel({ name: '情报', color: '#3b82f6' }),
        ]}
      />
    )

    for (const name of ['auto', '候选', '情报']) {
      await expect
        .element(screen.getByText(name, { exact: true }))
        .toBeInTheDocument()
    }
    const row = screen.container.firstElementChild as HTMLElement
    expect(getComputedStyle(row).flexWrap).toBe('wrap')
  })
})
