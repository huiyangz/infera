import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { GitBranch, SlidersHorizontal } from 'lucide-react'
import { toast } from 'sonner'
import {
  getProject,
  getProjectPipeline,
  getProjectStageRuns,
  getProjectStats,
  listAgents,
  putProjectPipeline,
} from '@/lib/infera-api'
import { type BindingMap } from '@/lib/infera-types'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Header } from '@/components/layout/header'
import { BindingEditor } from '@/features/pipeline/binding-editor'
import { KpiHeader } from './dashboard/kpi-header'
import { ProjectTabs } from './dashboard/project-tabs'
import { StageStatsTable } from './dashboard/stage-stats'
import {
  AgentStageTimeline,
  TimelineSkeleton,
} from './dashboard/stage-timeline'
import { CreateRequirementDialog } from './requirement-create-dialog'

/**
 * 项目详情 = dashboard 总览（INFERA-243）：页内一级导航（总览/项目任务）
 * + KPI 统计区（T01 冻结契约，状态构成走占比条，不再与任何列表重复计数）
 * + Agent 执行时序区（T02 冻结契约 stage-runs）+ 必需配置。
 * 纯展示组件在 ./dashboard/（props 驱动、可单测），本文件只做取数与状态编排。
 */
export function ProjectDetail({ projectId }: { projectId: string }) {
  const { data: proj } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => getProject(projectId),
  })
  const statsQuery = useQuery({
    queryKey: ['project-stats', projectId],
    queryFn: () => getProjectStats(projectId),
  })
  const runsQuery = useQuery({
    queryKey: ['project-stage-runs', projectId],
    queryFn: () => getProjectStageRuns(projectId),
  })

  // Git 仓库行归类（INFERA-191 / INFERA-209）：repo_url 为 / 开头的本地
  // 路径时不再入卡（本地路径行已移除），仅 git 地址（https/ssh/git@，
  // 与后端 validRepoURL 白名单对齐）入行，空值给「未绑定」占位。
  const gitURL =
    proj?.repo_url && !proj.repo_url.startsWith('/') ? proj.repo_url : ''

  const runs = runsQuery.data?.runs ?? []
  const byStage = runsQuery.data?.by_stage ?? []

  return (
    <>
      <Header fixed>
        <div className='flex w-full min-w-0 items-center justify-between gap-4'>
          <div className='flex min-w-0 flex-col gap-1'>
            <div className='flex items-center gap-2 text-sm text-muted-foreground'>
              <Link to='/' className='hover:text-foreground'>
                项目
              </Link>
              <span>/</span>
              <span className='truncate font-medium text-foreground'>
                {proj?.name ?? <Skeleton className='h-4 w-24' />}
              </span>
            </div>
            <p className='flex items-center gap-1.5 font-mono text-xs text-muted-foreground'>
              <GitBranch className='size-3.5 shrink-0' />
              <span className='truncate' title={proj?.repo_url || undefined}>
                {proj?.repo_url || '（未绑仓库）'}
              </span>
              {proj?.default_branch && (
                <>
                  <span className='shrink-0 text-border'>·</span>
                  <span className='shrink-0'>{proj.default_branch}</span>
                </>
              )}
            </p>
          </div>
          <div className='flex shrink-0 items-center gap-2'>
            <CreateRequirementDialog projectId={projectId} />
            {proj && (
              <OrchestrationDialog
                projectId={projectId}
                projectName={proj.name}
              />
            )}
          </div>
        </div>
      </Header>

      {/* 项目域页内一级导航：任务列表入口从卡片角落小链接升级到这里 */}
      <ProjectTabs projectId={projectId} active='overview' />

      <div className='mx-auto w-full max-w-6xl space-y-6 p-6'>
        {/* 项目统计：dashboard 头部——KPI 讲总量与可行动量，状态构成走占比条 */}
        <section>
          <h2 className='mb-3 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
            项目统计
          </h2>
          {statsQuery.isError ? (
            <Card className='flex items-center justify-between gap-3 px-4 py-3'>
              <p className='text-sm text-muted-foreground'>统计数据加载失败</p>
              <Button
                size='sm'
                variant='ghost'
                onClick={() => statsQuery.refetch()}
              >
                重试
              </Button>
            </Card>
          ) : (
            <KpiHeader stats={statsQuery.data} />
          )}
        </section>

        {/* Agent 执行时序：甘特泳道 + 分 stage 聚合（契约冻结于 T02） */}
        <section>
          <h2 className='mb-3 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
            Agent 执行时序
          </h2>
          <Card className='gap-4 px-5 py-5'>
            {runsQuery.isPending ? (
              <TimelineSkeleton />
            ) : runsQuery.isError ? (
              <div className='flex items-center justify-between gap-3 py-6'>
                <p className='text-sm text-muted-foreground'>时序数据加载失败</p>
                <Button
                  size='sm'
                  variant='ghost'
                  onClick={() => runsQuery.refetch()}
                >
                  重试
                </Button>
              </div>
            ) : (
              <>
                <AgentStageTimeline runs={runs} />
                {byStage.length > 0 && (
                  <div className='space-y-2.5'>
                    <Separator />
                    <p className='text-xs text-muted-foreground'>阶段耗时</p>
                    <StageStatsTable byStage={byStage} />
                  </div>
                )}
              </>
            )}
          </Card>
        </section>

        {/* 必需配置：只读呈现项目已有配置字段（INFERA-209 移除本地路径行） */}
        <section>
          <h2 className='mb-3 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
            必需配置
          </h2>
          <Card className='gap-0 py-2'>
            <dl className='divide-y'>
              <div className='flex items-center justify-between gap-4 px-5 py-3'>
                <dt className='shrink-0 text-sm text-muted-foreground'>
                  Git 仓库
                </dt>
                <dd className='min-w-0 truncate font-mono text-sm'>
                  {gitURL || (
                    <span className='text-muted-foreground'>未绑定</span>
                  )}
                </dd>
              </div>
              <div className='flex items-center justify-between gap-4 px-5 py-3'>
                <dt className='shrink-0 text-sm text-muted-foreground'>
                  默认分支
                </dt>
                <dd className='min-w-0 truncate font-mono text-sm'>
                  {proj?.default_branch || (
                    <span className='text-muted-foreground'>—</span>
                  )}
                </dd>
              </div>
            </dl>
          </Card>
        </section>
      </div>
    </>
  )
}

/**
 * 项目编排对话框：项目级唯一绑定定义（全局默认编排已删除，INFERA-181）。
 * 展示当前项目绑定（{nodes, bindings} 契约），全量另存，
 * 或「清空项目编排」清空全部绑定（PUT {}）。
 */
function OrchestrationDialog({
  projectId,
  projectName,
}: {
  projectId: string
  projectName: string
}) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  // sel=null 表示未编辑，展示后端当前项目绑定；用户改动后暂存本地，保存成功即复位
  const [sel, setSel] = useState<BindingMap | null>(null)

  const { data: pipe } = useQuery({
    queryKey: ['project-pipeline', projectId],
    queryFn: () => getProjectPipeline(projectId),
    enabled: open,
  })
  const { data: agents } = useQuery({
    queryKey: ['agents'],
    queryFn: () => listAgents(),
    enabled: open,
  })

  const value = sel ?? pipe?.bindings ?? {}

  const save = useMutation({
    mutationFn: (bindings: BindingMap) =>
      putProjectPipeline(projectId, bindings),
    onSuccess: (_d, bindings) => {
      toast.success(
        Object.keys(bindings).length ? '项目编排已保存' : '已清空项目编排'
      )
      setSel(null)
      qc.invalidateQueries({ queryKey: ['project-pipeline', projectId] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const nodes = pipe?.nodes ?? []
  const allChosen = nodes.length > 0 && nodes.every((n) => value[n])

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant='ghost' size='icon' aria-label='编排'>
          <SlidersHorizontal />
        </Button>
      </DialogTrigger>
      <DialogContent className='max-w-2xl'>
        <DialogHeader>
          <DialogTitle>流水线编排 · {projectName}</DialogTitle>
          <DialogDescription>
            为本项目各节点指定执行 Agent（项目绑定是唯一绑定定义）
          </DialogDescription>
        </DialogHeader>
        <BindingEditor
          nodes={nodes}
          agents={agents ?? []}
          value={value}
          onChange={setSel}
        />
        <DialogFooter className='sm:justify-between'>
          <Button
            variant='ghost'
            disabled={save.isPending}
            onClick={() => save.mutate({})}
          >
            清空项目编排
          </Button>
          <Button
            disabled={save.isPending || !allChosen}
            onClick={() => save.mutate(value)}
          >
            {save.isPending ? '保存中…' : '保存项目编排'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
