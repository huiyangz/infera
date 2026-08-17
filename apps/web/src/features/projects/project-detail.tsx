import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { GitBranch, Inbox, Plus } from 'lucide-react'
import { toast } from 'sonner'
import {
  createDelivery,
  getProject,
  listProjectDeliveries,
} from '@/lib/infera-api'
import { stageLabel } from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Header } from '@/components/layout/header'
import { StatusBadge } from '@/components/status-badge'

export function ProjectDetail({ projectId }: { projectId: string }) {
  const qc = useQueryClient()
  const { data: proj } = useQuery({
    queryKey: ['project', projectId],
    queryFn: () => getProject(projectId),
  })
  const { data: deliveries, isLoading } = useQuery({
    queryKey: ['project-deliveries', projectId],
    queryFn: () => listProjectDeliveries(projectId),
  })
  const [title, setTitle] = useState('')

  const create = useMutation({
    mutationFn: () => createDelivery(projectId, { title }),
    onSuccess: () => {
      setTitle('')
      toast.success('需求已提交，流水线即将启动')
      qc.invalidateQueries({ queryKey: ['project-deliveries', projectId] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const waiting = deliveries?.filter((d) => d.pending_gate).length ?? 0

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center justify-between gap-4'>
          <div className='flex flex-col gap-1'>
            <div className='flex items-center gap-2 text-sm text-muted-foreground'>
              <Link to='/' className='hover:text-foreground'>
                项目
              </Link>
              <span>/</span>
              <span className='font-medium text-foreground'>
                {proj?.name ?? <Skeleton className='h-4 w-24' />}
              </span>
            </div>
            <p className='flex items-center gap-1.5 font-mono text-xs text-muted-foreground'>
              <GitBranch className='size-3.5' />
              {proj?.repo_url || '（未绑仓库）'}
              <span className='text-border'>·</span>
              {proj?.default_branch}
            </p>
          </div>
          <form
            className='flex items-center gap-2'
            onSubmit={(e) => {
              e.preventDefault()
              if (title.trim()) create.mutate()
            }}
          >
            <Input
              className='w-64'
              placeholder='一句话需求，回车提交…'
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
            <Button
              type='submit'
              size='lg'
              disabled={!title.trim() || create.isPending}
            >
              <Plus /> 新建交付
            </Button>
          </form>
        </div>
      </Header>

      <div className='p-6'>
        <Card>
          <CardContent className='p-0'>
            {isLoading ? (
              <div className='space-y-2 p-6'>
                <Skeleton className='h-10 w-full' />
                <Skeleton className='h-10 w-full' />
              </div>
            ) : !deliveries?.length ? (
              <div className='flex flex-col items-center gap-3 p-16 text-center'>
                <div className='flex size-12 items-center justify-center rounded-full bg-muted'>
                  <Inbox className='size-6 text-muted-foreground' />
                </div>
                <div>
                  <p className='font-medium'>还没有需求</p>
                  <p className='mt-1 text-sm text-muted-foreground'>
                    在右上角输入一句话需求，流水线会自动接管
                  </p>
                </div>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>需求</TableHead>
                    <TableHead>当前阶段</TableHead>
                    <TableHead>状态</TableHead>
                    <TableHead className='text-right'>
                      {waiting > 0 ? (
                        <span className='text-foreground'>
                          {waiting} 个待审批
                        </span>
                      ) : (
                        '更新时间'
                      )}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {deliveries.map((d) => (
                    <TableRow key={d.id} className='relative cursor-pointer'>
                      <TableCell className='font-medium'>
                        <Link
                          to='/deliveries/$id'
                          params={{ id: d.id }}
                          className='after:absolute after:inset-0'
                        >
                          {d.title}
                        </Link>
                        {d.pending_gate ? (
                          <Badge className='relative z-10 ml-2'>
                            <Link
                              to='/deliveries/$id/gate'
                              params={{ id: d.id }}
                              className='after:absolute after:inset-0'
                            >
                              待审批
                            </Link>
                          </Badge>
                        ) : d.fail_count > 0 ? (
                          <span className='ml-2 align-middle text-xs text-muted-foreground'>
                            回环 {d.fail_count} 次
                          </span>
                        ) : null}
                      </TableCell>
                      <TableCell className='text-sm text-muted-foreground'>
                        {stageLabel(d.current_stage)}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={d.status} />
                      </TableCell>
                      <TableCell className='text-right text-xs text-muted-foreground tabular-nums'>
                        {timeAgo(d.updated_at)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </div>
    </>
  )
}
