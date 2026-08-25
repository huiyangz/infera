import { render, cleanup } from 'vitest-browser-react'
import { afterEach, describe, expect, it, vi } from 'vitest'

// Link 脱离 Router 上下文无法渲染，用 <a> 替身（带 $id 参数替换）；
// to/params/activeOptions 为 Link 自有 props，真实实现不透传 DOM，替身同样剥掉
vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  const MockLink = ({
    children,
    to,
    params,
    activeOptions: _activeOptions,
    ...props
  }: React.ComponentProps<'a'> & {
    to?: string
    params?: Record<string, string>
    activeOptions?: unknown
  }) => (
    <a href={(to ?? '#').replace('$id', params?.id ?? '')} {...props}>
      {children}
    </a>
  )
  return { ...actual, Link: MockLink }
})

import { ProjectTabs } from './project-tabs'

afterEach(async () => {
  await cleanup()
})

describe('ProjectTabs（项目域页内一级导航）', () => {
  it('总览 / 项目任务两个入口，分别链到 /projects/{id} 与 /projects/{id}/tasks', async () => {
    const screen = await render(<ProjectTabs projectId='p1' />)

    const nav = await screen.getByRole('navigation', { name: '项目导航' }).element()
    const links = nav?.querySelectorAll('a') ?? []
    expect(links.length).toBe(2)
    expect(links[0]?.getAttribute('href')).toBe('/projects/p1')
    expect(links[1]?.getAttribute('href')).toBe('/projects/p1/tasks')
  })

  it('默认总览页签激活：aria-current 标注当前页', async () => {
    const screen = await render(<ProjectTabs projectId='p1' />)

    const overview = await screen.getByRole('link', { name: /总览/ }).element()
    expect(overview?.getAttribute('aria-current')).toBe('page')
    const tasks = await screen.getByRole('link', { name: /项目任务/ }).element()
    expect(tasks?.getAttribute('aria-current')).toBeNull()
  })

  it('active 可切换：任务页态下「项目任务」页签激活', async () => {
    const screen = await render(<ProjectTabs projectId='p1' active='tasks' />)

    const tasks = await screen.getByRole('link', { name: /项目任务/ }).element()
    expect(tasks?.getAttribute('aria-current')).toBe('page')
    const overview = await screen.getByRole('link', { name: /总览/ }).element()
    expect(overview?.getAttribute('aria-current')).toBeNull()
  })
})
