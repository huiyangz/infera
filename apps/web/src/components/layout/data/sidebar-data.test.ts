import { describe, expect, it } from 'vitest'
import { sidebarData } from './sidebar-data'

/** 全部导航项摊平（含子项） */
function allItems() {
  return sidebarData.navGroups.flatMap((g) =>
    g.items.flatMap((item) => [
      item,
      ...(item.items?.map((sub) => ({ ...sub, title: sub.title })) ?? []),
    ])
  )
}

describe('全局导航（AC：Agent 入口完全移除 + 需要决策入口可达）', () => {
  it('不含任何指向 /agents 的导航项（含命令面板同源数据）', () => {
    const urls = allItems().map((i) => i.url)
    expect(urls).not.toContain('/agents')
    expect(urls.join(' ')).not.toMatch(/\/agents/)
  })

  it('含「需要决策」顶层入口，指向 /decisions', () => {
    const top = sidebarData.navGroups.flatMap((g) => g.items)
    const entry = top.find((i) => i.url === '/decisions')
    expect(entry?.title).toBe('需要决策')
  })

  it('含「需求发现」顶层入口，指向 /discovery（INFERA-226）', () => {
    const top = sidebarData.navGroups.flatMap((g) => g.items)
    const entry = top.find((i) => i.url === '/discovery')
    expect(entry?.title).toBe('需求发现')
  })

  it('不含任何指向 /agent-activity 的导航项——可视化已迁入项目详情页签（INFERA-259）', () => {
    const urls = allItems().map((i) => i.url)
    expect(urls).not.toContain('/agent-activity')
    expect(urls.join(' ')).not.toMatch(/agent-activity/)
    expect(
      allItems().some((i) => i.title.includes('Agent 执行时序'))
    ).toBe(false)
  })
})
