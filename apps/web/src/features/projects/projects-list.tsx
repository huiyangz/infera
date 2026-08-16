import { useEffect, useState } from 'react'
import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { FolderGit2, Pin, PinOff, Plus } from 'lucide-react'
import { toast } from 'sonner'
import {
  createProject,
  listProjectDeliveries,
  listProjects,
} from '@/lib/infera-api'
import type { Delivery, Project } from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import { cn } from '@/lib/utils'
import { Header } from '@/components/layout/header'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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

const PIN_KEY = 'infera:pinned-projects'

function loadPins(): string[] {
  try {
    return JSON.parse(localStorage.getItem(PIN_KEY) ?? '[]')
  } catch {
    return []
  }
}

/** 项目卡上的实时摘要：活跃数 / 待审批 / 最近活动 */
function projectSummary(deliveries: Delivery[] | undefined, proj: Project) {
  if (!deliveries)
    return { active: undefined, pending: undefined, last: proj.updated_at }
  const active = deliveries.filter((d) => d.status === 'active').length
  const pending = deliveries.filter((d) => d.pending_gate).length
  const last = deliveries.reduce(
    (m, d) => (d.updated_at > m ? d.updated_at : m),
    proj.updated_at,
  )
  return { active, pending, last }
}

function ProjectCard({
  proj,
  deliveries,
  pinned,
  onTogglePin,
}: {
  proj: Project
  deliveries: Delivery[] | undefined
  pinned: boolean
  onTogglePin: () => void
}) {
  const s = projectSummary(deliveries, proj)
  return (
    <Card className='group relative gap-3 py-5 transition-colors hover:bg-accent/50'>
      <CardHeader className='px-5'>
        <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
          <Link
            to='/projects/$id'
            params={{ id: proj.id }}
            className='after:absolute after:inset-0'
          >
            {proj.name}
          </Link>
        </CardTitle>
        <CardAction className='relative z-10'>
          <Button
            variant='ghost'
            size='icon'
            aria-label={pinned ? '取消置顶' : '置顶'}
            onClick={onTogglePin}
          >
            {pinned ? (
              <PinOff />
            ) : (
              <Pin className='opacity-0 transition-opacity group-hover:opacity-100' />
            )}
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className='grid gap-2 px-5'>
        <p className='truncate font-mono text-xs text-muted-foreground'>
          {proj.repo_url || '（未绑仓库）'}
        </p>
        <div className='flex min-h-6 flex-wrap items-center gap-1.5'>
          {s.pending ? (
            <Badge>
              <Link
                to='/projects/$id'
                params={{ id: proj.id }}
                className='after:absolute after:inset-0'
              >
                {s.pending} 个待审批
              </Link>
            </Badge>
          ) : null}
          {s.active ? (
            <Badge variant='outline' className='gap-1.5'>
              <span className='size-1.5 animate-pulse rounded-full bg-foreground' />
              {s.active} 个进行中
            </Badge>
          ) : null}
          {!s.pending && !s.active && deliveries && (
            <span className='text-xs text-muted-foreground'>没有活跃需求</span>
          )}
          {!deliveries && <Skeleton className='h-5 w-24' />}
        </div>
        <p className='text-xs tabular-nums text-muted-foreground'>
          活动 {timeAgo(s.last)}
        </p>
      </CardContent>
    </Card>
  )
}

export function ProjectsList() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['projects'],
    queryFn: listProjects,
  })
  const [pinned, setPinned] = useState<string[]>([])
  useEffect(() => setPinned(loadPins()), [])

  const togglePin = (id: string) => {
    setPinned((prev) => {
      const next = prev.includes(id)
        ? prev.filter((p) => p !== id)
        : [...prev, id]
      localStorage.setItem(PIN_KEY, JSON.stringify(next))
      return next
    })
  }

  // 并发拉每个项目的需求列表（项目数少，聚合接口以后再加）
  const deliveriesQueries = useQueries({
    queries: (data ?? []).map((p) => ({
      queryKey: ['project-deliveries', p.id],
      queryFn: () => listProjectDeliveries(p.id),
    })),
  })
  const summaryOf = (proj: Project) => {
    const idx = data?.findIndex((d) => d.id === proj.id) ?? -1
    return projectSummary(idx >= 0 ? deliveriesQueries[idx]?.data : undefined, proj)
  }

  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [repo, setRepo] = useState('')
  const [branch, setBranch] = useState('main')

  const create = useMutation({
    mutationFn: () =>
      createProject({ name, repo_url: repo, default_branch: branch }),
    onSuccess: (p) => {
      setOpen(false)
      setName('')
      setRepo('')
      toast.success(`项目 ${p.name} 已创建`)
      qc.invalidateQueries({ queryKey: ['projects'] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  // 排序：置顶在前（按置顶先后），其余按最近活动倒序
  const sorted = [...(data ?? [])].sort((a, b) => {
    const pa = pinned.indexOf(a.id)
    const pb = pinned.indexOf(b.id)
    if (pa !== -1 || pb !== -1) {
      if (pa === -1) return 1
      if (pb === -1) return -1
      return pa - pb
    }
    return summaryOf(b).last.localeCompare(summaryOf(a).last)
  })

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center justify-between'>
          <div className='flex flex-col gap-1'>
            <h1 className='text-3xl font-semibold tracking-[-0.9px]'>项目</h1>
            <p className='text-sm text-muted-foreground'>
              每个项目对应一个代码仓库，需求在项目内流转交付
            </p>
          </div>
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger asChild>
              <Button size='lg'>
                <Plus /> 新建项目
              </Button>
            </DialogTrigger>
            <DialogContent className='sm:max-w-md'>
              <DialogHeader>
                <DialogTitle>新建项目</DialogTitle>
                <DialogDescription>
                  绑定仓库会先试 clone 校验可达性与写权限
                </DialogDescription>
              </DialogHeader>
              <form
                className='grid gap-4'
                onSubmit={(e) => {
                  e.preventDefault()
                  if (name.trim()) create.mutate()
                }}
              >
                <div className='grid gap-2'>
                  <Label htmlFor='proj-name'>项目名</Label>
                  <Input
                    id='proj-name'
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    placeholder='my-project'
                  />
                </div>
                <div className='grid gap-2'>
                  <Label htmlFor='proj-repo'>
                    Git 仓库（留空 = 绿地新项目）
                  </Label>
                  <Input
                    id='proj-repo'
                    className='font-mono text-xs'
                    value={repo}
                    onChange={(e) => setRepo(e.target.value)}
                    placeholder='https://github.com/you/repo.git'
                  />
                </div>
                <div className='grid gap-2'>
                  <Label htmlFor='proj-branch'>默认分支</Label>
                  <Input
                    id='proj-branch'
                    className='font-mono text-xs'
                    value={branch}
                    onChange={(e) => setBranch(e.target.value)}
                  />
                </div>
                <DialogFooter>
                  <Button type='submit' disabled={!name.trim() || create.isPending}>
                    创建并绑定
                  </Button>
                </DialogFooter>
              </form>
            </DialogContent>
          </Dialog>
        </div>
      </Header>

      <div className='p-6'>
        {isLoading ? (
          <div className={cn('grid gap-4 sm:grid-cols-2 lg:grid-cols-3')}>
            <Skeleton className='h-40 rounded-lg' />
            <Skeleton className='h-40 rounded-lg' />
            <Skeleton className='h-40 rounded-lg' />
          </div>
        ) : !data?.length ? (
          <div className='flex flex-col items-center gap-3 p-16 text-center'>
            <div className='flex size-12 items-center justify-center rounded-full bg-muted'>
              <FolderGit2 className='size-6 text-muted-foreground' />
            </div>
            <div>
              <p className='font-medium'>还没有项目</p>
              <p className='mt-1 text-sm text-muted-foreground'>
                点右上角「新建项目」，绑定仓库后即可提交需求
              </p>
            </div>
          </div>
        ) : (
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
            {sorted.map((p) => (
              <ProjectCard
                key={p.id}
                proj={p}
                deliveries={summaryOf(p).active === undefined ? undefined : deliveriesQueries[data.findIndex((d) => d.id === p.id)]?.data}
                pinned={pinned.includes(p.id)}
                onTogglePin={() => togglePin(p.id)}
              />
            ))}
          </div>
        )}
      </div>
    </>
  )
}
