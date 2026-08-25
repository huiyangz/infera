import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { getProject } from '@/lib/infera-api'
import { Header } from '@/components/layout/header'
import { Skeleton } from '@/components/ui/skeleton'
import { AgentActivityPanel } from '@/features/agent-activity/agent-activity-panel'
import { ProjectTabs } from './dashboard/project-tabs'

/**
 * 项目详情「Agent 执行时序」tab 页（INFERA-259）：原独立路由 /agent-activity
 * 的可视化主体（图表 + 窗口控件）原样迁入，作为项目域页内一级导航的第三个
 * 页签。数据口径保持工作区全局（GET /api/agent-activity 契约冻结于
 * INFERA-253，不按项目过滤）——页头口径说明如实标注「跨项目」，
 * 页头不再重复独立页时期的 h1 标题，页签即标题。
 */
export function ProjectAgentActivity({ projectId }: { projectId: string }) {
  const { data: proj } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => getProject(projectId),
  })

  return (
    <>
      <Header fixed>
        <div className='flex w-full min-w-0 items-start justify-between gap-4'>
          <div className='flex min-w-0 flex-col gap-1'>
            <div className='flex items-center gap-2 text-sm text-muted-foreground'>
              <Link to='/' className='hover:text-foreground'>
                项目
              </Link>
              <span>/</span>
              <Link
                to='/projects/$id'
                params={{ id: projectId }}
                className='truncate hover:text-foreground'
              >
                {proj?.name ?? <Skeleton className='h-4 w-24' />}
              </Link>
              <span>/</span>
              <span className='truncate font-medium text-foreground'>
                Agent 执行时序
              </span>
            </div>
            <p className='text-sm text-muted-foreground'>
              工作区各 agent 执行次数（跨项目统计），30 分钟桶
            </p>
          </div>
        </div>
      </Header>

      {/* 项目域页内一级导航：本页为第三个页签「Agent 执行时序」 */}
      <ProjectTabs projectId={projectId} active='agent-activity' />

      <div className='mx-auto w-full max-w-6xl p-6'>
        <AgentActivityPanel />
      </div>
    </>
  )
}
