import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { GitBranch, Inbox, Plus } from 'lucide-react'
import { toast } from 'sonner'
import {
  createDelivery,
  getProject,
  listProjectDeliveries,
} from '@/lib/infera-api'
import { type Delivery, stageLabel } from '@/lib/infera-types'
import { timeAgo } from '@/lib/time'
import { cn } from '@/lib/utils'
import { DeliveryDetail } from '@/features/deliveries/delivery-detail'
import { StatusBadge } from '@/components/status-badge'
import { Header } from '@/components/layout/header'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * 项目详情 = 主从布局：左侧本项目需求列表（可选中），右侧选中需求详情。
 * 选中态走 URL search param（?d=），可分享、可后退；移动端只显示一栏。
 */
export function ProjectDetail({
  projectId,
  selectedDeliveryId,
}: {
  projectId: string
  selectedDeliveryId?: string
}) {
  const navigate = useNavigate()
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
    onSuccess: (d) => {
      setTitle('')
      toast.success('需求已提交，流水线即将启动')
      qc.invalidateQueries({ queryKey: ['project-deliveries', projectId] })
      navigate({ to: '/projects/$id', params: { id: projectId }, search: { d: d.id } })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  // URL 未指定时默认选中第一条（仅影响渲染，不写 URL）
  const effectiveId = selectedDeliveryId ?? deliveries?.[0]?.id
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

      <div className='flex h-[calc(100svh-4rem)]'>
        {/* 左：需求列表（通栏，hairline 分隔，不做卡片盒） */}
        <aside
          className={cn(
            'flex w-80 shrink-0 flex-col border-r',
            effectiveId && 'max-lg:hidden',
          )}
        >
          <div className='flex h-9 items-center justify-between border-b px-4'>
            <span className='text-xs font-medium uppercase tracking-wider text-muted-foreground'>
              需求
              {deliveries?.length ? (
                <span className='ml-1.5'>
                  {deliveries.length}
                </span>
              ) : null}
            </span>
            {waiting > 0 && (
              <span className='text-xs text-foreground'>{waiting} 个待审批</span>
            )}
          </div>
          <div className='flex-1 overflow-y-auto'>
            {isLoading ? (
              <div className='space-y-2 p-3'>
                <Skeleton className='h-14 w-full' />
                <Skeleton className='h-14 w-full' />
              </div>
            ) : !deliveries?.length ? (
              <div className='flex flex-col items-center gap-2 p-8 text-center'>
                <Inbox className='size-5 text-muted-foreground' />
                <p className='text-sm text-muted-foreground'>
                  还没有需求，右上角输入一句话提交
                </p>
              </div>
            ) : (
              deliveries.map((d) => (
                <DeliveryRow
                  key={d.id}
                  d={d}
                  active={d.id === effectiveId}
                  onSelect={() =>
                    navigate({
                      to: '/projects/$id',
                      params: { id: projectId },
                      search: {
                        d: d.id === effectiveId ? undefined : d.id,
                      },
                    })
                  }
                />
              ))
            )}
          </div>
        </aside>

        {/* 右：详情面板（通栏阅读面） */}
        <main
          className={cn(
            'min-w-0 flex-1 overflow-y-auto',
            !effectiveId && 'max-lg:hidden',
          )}
        >
          {effectiveId ? (
            <DeliveryDetail deliveryId={effectiveId} embedded />
          ) : null}
        </main>
      </div>
    </>
  )
}

function DeliveryRow({
  d,
  active,
  onSelect,
}: {
  d: Delivery
  active: boolean
  onSelect: () => void
}) {
  return (
    <button
      type='button'
      onClick={onSelect}
      className={cn(
        'relative block w-full border-b px-4 py-2.5 text-start transition-colors last:border-b-0',
        active ? 'bg-accent' : 'hover:bg-accent/50',
      )}
    >
      {active && (
        <span className='absolute inset-y-0 start-0 w-0.5 bg-foreground' />
      )}
      <span className='flex items-center justify-between gap-2'>
        <span className='truncate text-sm font-medium'>{d.title}</span>
        <StatusBadge status={d.status} />
      </span>
      <span className='mt-1 flex items-center justify-between text-xs text-muted-foreground'>
        <span>{stageLabel(d.current_stage)}</span>
        <span className='tabular-nums'>{timeAgo(d.updated_at)}</span>
      </span>
      {d.pending_gate && (
        <span className='mt-1.5 inline-block rounded-full bg-primary px-2 py-0.5 text-[11px] font-medium text-primary-foreground'>
          待审批
        </span>
      )}
    </button>
  )
}
