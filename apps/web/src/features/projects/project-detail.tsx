import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  ChevronRight,
  GitBranch,
  ListTree,
  SlidersHorizontal,
} from 'lucide-react'
import { toast } from 'sonner'
import {
  getProject,
  getProjectPipeline,
  getProjectStats,
  listAgents,
  putProjectPipeline,
} from '@/lib/infera-api'
import { type BindingMap, type RequirementStats } from '@/lib/infera-types'
import { dateTime } from '@/lib/time'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Header } from '@/components/layout/header'
import { BindingEditor } from '@/features/pipeline/binding-editor'
import { CreateRequirementDialog } from './requirement-create-dialog'

/** 状态桶展示口径（与 StatusBadge 一致的中文名） */
const STATUS_BUCKETS: Array<{
  key: keyof RequirementStats['by_status']
  label: string
}> = [
  { key: 'active', label: '进行中' },
  { key: 'queued', label: '未启动' },
  { key: 'completed', label: '已完成' },
  { key: 'blocked', label: '已阻塞' },
]

/**
 * 项目详情 = 项目域总览（L202608221241-2-T04）：必需配置 + 项目统计
 * （T01 冻结契约）+ 任务列表入口。需求/任务浏览移至项目任务列表页。
 */
export function ProjectDetail({ projectId }: { projectId: string }) {
  const { data: proj } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => getProject(projectId),
  })
  const { data: stats } = useQuery({
    queryKey: ['project-stats', projectId],
    queryFn: () => getProjectStats(projectId),
  })

  // 必需配置双行归类（INFERA-191）：repo_url 单字段只承载其一（/ 开头 =
  // 本地目录绝对路径，https/ssh/git@ = git 仓库地址，与后端 validRepoURL
  // 白名单对齐），按形态择一入行，另一行给「未绑定」占位。
  const localPath = proj?.repo_url.startsWith('/') ? proj.repo_url : ''
  const gitURL =
    proj?.repo_url && !proj.repo_url.startsWith('/') ? proj.repo_url : ''

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

      <div className='mx-auto w-full max-w-4xl space-y-6 p-6'>
        {/* 必需配置：只读呈现项目已有配置字段（INFERA-191 双行） */}
        <section>
          <h2 className='mb-3 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
            必需配置
          </h2>
          <Card className='gap-0 py-2'>
            <dl className='divide-y'>
              <div className='flex items-center justify-between gap-4 px-5 py-3'>
                <dt className='shrink-0 text-sm text-muted-foreground'>
                  本地路径
                </dt>
                <dd className='min-w-0 truncate font-mono text-sm'>
                  {localPath || (
                    <span className='text-muted-foreground'>未绑定</span>
                  )}
                </dd>
              </div>
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

        {/* 项目统计：T01 冻结契约（store.RequirementStats） */}
        <section>
          <h2 className='mb-3 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
            项目统计
          </h2>
          <div className='grid gap-4 sm:grid-cols-3'>
            <Card className='gap-1 py-5'>
              <CardHeader className='px-5'>
                <CardTitle className='text-sm font-normal text-muted-foreground'>
                  任务总数
                </CardTitle>
              </CardHeader>
              <CardContent className='px-5'>
                <p className='text-2xl font-semibold tabular-nums'>
                  {stats ? (
                    stats.requirement_total
                  ) : (
                    <Skeleton className='h-8 w-10' />
                  )}
                </p>
              </CardContent>
            </Card>
            <Card className='gap-1 py-5'>
              <CardHeader className='px-5'>
                <CardTitle className='text-sm font-normal text-muted-foreground'>
                  待决策
                </CardTitle>
              </CardHeader>
              <CardContent className='px-5'>
                <p className='text-2xl font-semibold tabular-nums'>
                  {stats ? (
                    stats.pending_decisions
                  ) : (
                    <Skeleton className='h-8 w-10' />
                  )}
                </p>
              </CardContent>
            </Card>
            <Card className='gap-1 py-5'>
              <CardHeader className='px-5'>
                <CardTitle className='text-sm font-normal text-muted-foreground'>
                  已交付
                </CardTitle>
              </CardHeader>
              <CardContent className='px-5'>
                <p className='text-2xl font-semibold tabular-nums'>
                  {stats ? stats.delivered : <Skeleton className='h-8 w-10' />}
                </p>
              </CardContent>
            </Card>
          </div>
          <Card className='mt-4 gap-0 py-2'>
            <dl className='divide-y'>
              {STATUS_BUCKETS.map(({ key, label }) => (
                <div
                  key={key}
                  className='flex items-center justify-between gap-4 px-5 py-2.5'
                >
                  <dt className='text-sm text-muted-foreground'>{label}</dt>
                  <dd className='text-sm font-medium tabular-nums'>
                    {stats ? (
                      stats.by_status[key]
                    ) : (
                      <Skeleton className='h-4 w-8' />
                    )}
                  </dd>
                </div>
              ))}
              <div className='flex items-center justify-between gap-4 px-5 py-2.5'>
                <dt className='text-sm text-muted-foreground'>最近活动</dt>
                <dd className='text-sm text-muted-foreground tabular-nums'>
                  {stats ? (
                    stats.last_synced_at ? (
                      dateTime(stats.last_synced_at)
                    ) : (
                      '暂无活动'
                    )
                  ) : (
                    <Skeleton className='h-4 w-20' />
                  )}
                </dd>
              </div>
            </dl>
          </Card>
        </section>

        {/* 任务列表入口 */}
        <section>
          <h2 className='mb-3 text-xs font-medium tracking-wider text-muted-foreground uppercase'>
            任务列表
          </h2>
          <Card className='group py-5 transition-colors hover:bg-accent/50'>
            <CardHeader className='px-5'>
              <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
                <Link
                  to='/projects/$id/tasks'
                  params={{ id: projectId }}
                  className='after:absolute after:inset-0'
                >
                  项目任务
                </Link>
              </CardTitle>
              <CardAction className='relative z-10'>
                <Button
                  variant='ghost'
                  size='icon'
                  aria-label='项目任务'
                  asChild
                >
                  <Link to='/projects/$id/tasks' params={{ id: projectId }}>
                    <ListTree />
                  </Link>
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className='px-5'>
              <p className='flex items-center gap-1 text-sm text-muted-foreground'>
                以父子结构查看本项目的父任务与子任务
                <ChevronRight className='size-4' />
              </p>
            </CardContent>
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
