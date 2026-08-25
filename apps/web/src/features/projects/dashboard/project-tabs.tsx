import { Link } from '@tanstack/react-router'
import { LayoutDashboard, ListTree } from 'lucide-react'
import { cn } from '@/lib/utils'

/**
 * 项目域页内一级导航（INFERA-243）：总览 / 项目任务两页签，
 * 取代旧版任务分组卡片角落里的「项目任务」小链接。
 * 编辑风页签语言：hairline 底线 + 墨色下划线标当前页（DESIGN.md
 * 无信号色原则），路由各自独立（/projects/{id} 与 /projects/{id}/tasks）。
 */
export function ProjectTabs({
  projectId,
  active = 'overview',
}: {
  projectId: string
  active?: 'overview' | 'tasks'
}) {
  const tabs = [
    {
      key: 'overview' as const,
      to: '/projects/$id',
      label: '总览',
      icon: LayoutDashboard,
    },
    {
      key: 'tasks' as const,
      to: '/projects/$id/tasks',
      label: '项目任务',
      icon: ListTree,
    },
  ]

  return (
    <nav aria-label='项目导航' className='border-b'>
      <div className='mx-auto flex w-full max-w-6xl gap-1 px-6'>
        {tabs.map(({ key, to, label, icon: Icon }) => {
          const isCurrent = key === active
          return (
            <Link
              key={key}
              to={to}
              params={{ id: projectId }}
              aria-current={isCurrent ? 'page' : undefined}
              className={cn(
                'inline-flex items-center gap-1.5 border-b-2 px-3 py-2.5 text-sm font-medium',
                isCurrent
                  ? 'border-foreground text-foreground'
                  : 'border-transparent text-muted-foreground hover:text-foreground'
              )}
            >
              <Icon className='size-4' />
              {label}
            </Link>
          )
        })}
      </div>
    </nav>
  )
}
