import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { FolderGit2, Pin, PinOff, Plus } from 'lucide-react'
import { toast } from 'sonner'
import { createProject, listProjects, patchProjectPinned } from '@/lib/infera-api'
import type { Project } from '@/lib/infera-types'
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

function ProjectCard({
  proj,
  onTogglePin,
}: {
  proj: Project
  onTogglePin: () => void
}) {
  const s = proj.stats
  return (
    <Card className='group relative gap-3 py-5 transition-colors hover:bg-accent/50'>
      <CardHeader className='px-5'>
        <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
          <Link
            to='/projects/$id'
            params={{ id: proj.id }}
            search={{ d: undefined }}
            className='after:absolute after:inset-0'
          >
            {proj.name}
          </Link>
        </CardTitle>
        <CardAction className='relative z-10'>
          <Button
            variant='ghost'
            size='icon'
            aria-label={proj.pinned ? '取消置顶' : '置顶'}
            onClick={onTogglePin}
          >
            {proj.pinned ? (
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
          {s?.pending ? (
            <Badge>
              <Link
                to='/projects/$id'
                params={{ id: proj.id }}
                search={{ d: undefined }}
                className='after:absolute after:inset-0'
              >
                {s.pending} 个待审批
              </Link>
            </Badge>
          ) : null}
          {s?.active ? (
            <Badge variant='outline' className='gap-1.5'>
              <span className='size-1.5 animate-pulse rounded-full bg-foreground' />
              {s.active} 个进行中
            </Badge>
          ) : null}
          {s && !s.pending && !s.active && (
            <span className='text-xs text-muted-foreground'>没有活跃需求</span>
          )}
          {!s && <Skeleton className='h-5 w-24' />}
        </div>
        <p className='text-xs tabular-nums text-muted-foreground'>
          活动 {timeAgo(s?.last_activity ?? proj.updated_at)}
        </p>
      </CardContent>
    </Card>
  )
}

export function ProjectsList() {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['projects'],
    queryFn: () => listProjects(true),
  })

  // 置顶走服务端持久化，本地先乐观更新缓存
  const togglePin = useMutation({
    mutationFn: ({ id, next }: { id: string; next: boolean }) =>
      patchProjectPinned(id, next),
    onMutate: async ({ id, next }) => {
      await qc.cancelQueries({ queryKey: ['projects'] })
      const prev = qc.getQueryData<Project[]>(['projects'])
      qc.setQueryData<Project[]>(
        ['projects'],
        prev?.map((p) => (p.id === id ? { ...p, pinned: next } : p)),
      )
      return { prev }
    },
    onError: (e: Error, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(['projects'], ctx.prev)
      toast.error(e.message)
    },
  })

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

  // 排序：置顶在前，其余按最近活动倒序
  const sorted = [...(data ?? [])].sort((a, b) => {
    if (a.pinned !== b.pinned) return a.pinned ? -1 : 1
    const la = a.stats?.last_activity ?? a.updated_at
    const lb = b.stats?.last_activity ?? b.updated_at
    return lb.localeCompare(la)
  })

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center justify-between'>
          <div className='flex flex-col gap-1'>
            <h1 className='text-lg font-semibold tracking-[-0.2px]'>项目</h1>
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
                  <Label htmlFor='proj-repo'>Git 仓库（必填）</Label>
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
                  <Button type='submit' disabled={!name.trim() || !repo.trim() || create.isPending}>
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
                onTogglePin={() => togglePin.mutate({ id: p.id, next: !p.pinned })}
              />
            ))}
          </div>
        )}
      </div>
    </>
  )
}
