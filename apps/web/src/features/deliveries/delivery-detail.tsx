import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Check, ChevronLeft, Circle, DoorOpen, Loader2 } from 'lucide-react'
import { getDelivery } from '@/lib/infera-api'
import { useDeliveryEvents } from '@/hooks/use-delivery-events'
import {
  GATES,
  STAGES,
  type StageName,
  type TimelineEvent,
} from '@/lib/infera-types'
import { Header } from '@/components/layout/header'
import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { timeAgo } from '@/lib/time'

const STAGE_META: Record<StageName, { label: string; hint: string }> = {
  intake: { label: '需求受理', hint: '记录需求原文，建立交付档案' },
  spec: { label: '规格生成', hint: 'Spec Agent 依据需求与仓库代码撰写规格说明' },
  spec_approval: { label: '规格审批', hint: '人工门禁：确认规格无误后流水线继续' },
  test_gen: { label: '测试生成', hint: 'Test Agent 依据规格生成测试用例' },
  code_gen: { label: '实现', hint: 'Coder Agent 在仓库工作区内实现需求' },
  unit_test: { label: '单元测试', hint: '在容器中运行测试，失败自动回环至实现' },
  code_review: { label: '审查与交付', hint: 'Reviewer Agent 预审，人工确认后开出 PR' },
}

type StageState = 'done' | 'current' | 'gate-waiting' | 'pending'

const EVENT_LABEL: Record<string, string> = {
  delivery_created: '需求创建',
  stage_started: '阶段开始',
  stage_done: '阶段完成',
  gate_pending: '进入门禁',
  gate_approved: '审批通过',
  gate_rejected: '审批打回',
  test_failed: '测试失败',
  retry: '回环重试',
  blocked: '流水线阻塞',
}

function stageState(
  stage: StageName,
  currentIdx: number,
  idx: number,
  pendingGate: string | null,
): StageState {
  if (stage === pendingGate) return 'gate-waiting'
  if (idx < currentIdx) return 'done'
  if (idx === currentIdx) return 'current'
  return 'pending'
}

function StageIcon({ state }: { state: StageState }) {
  if (state === 'done') return <Check className='size-3.5' strokeWidth={2.5} />
  if (state === 'current' || state === 'gate-waiting')
    return <Loader2 className='size-3.5 animate-spin' />
  return <Circle className='size-3.5' />
}

export function DeliveryDetail({ deliveryId }: { deliveryId: string }) {
  useDeliveryEvents(deliveryId)
  const { data, isLoading } = useQuery({
    queryKey: ['delivery', deliveryId],
    queryFn: () => getDelivery(deliveryId),
  })

  if (isLoading || !data)
    return (
      <>
        <Header fixed>
          <Skeleton className='h-4 w-16' />
          <Skeleton className='mt-2 h-8 w-64' />
        </Header>
        <div className='mx-auto max-w-4xl space-y-6 p-6'>
          <Skeleton className='h-36 w-full rounded-xl' />
          <Skeleton className='h-72 w-full rounded-xl' />
        </div>
      </>
    )

  const { delivery, timeline } = data
  const currentIdx = STAGES.indexOf(delivery.current_stage as StageName)
  const eventsOf = (s: StageName) => timeline.filter((e) => e.stage === s)
  const doneCount = STAGES.filter(
    (s, i) => stageState(s, currentIdx, i, delivery.pending_gate) === 'done',
  ).length

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-start justify-between gap-4'>
          <div className='min-w-0'>
            <div className='flex items-center gap-1 text-sm text-muted-foreground'>
              <ChevronLeft className='size-4' />
              <Link
                to='/projects/$id'
                params={{ id: delivery.project_id }}
                className='hover:text-foreground'
              >
                返回项目
              </Link>
            </div>
            <h1 className='mt-1 truncate text-3xl font-semibold tracking-[-0.9px]'>
              {delivery.title}
            </h1>
            <p className='mt-1 max-w-2xl text-sm text-muted-foreground'>
              {delivery.description || '（无补充描述）'}
            </p>
          </div>
          <Badge
            variant={delivery.status === 'blocked' ? 'default' : 'secondary'}
            className='mt-1 shrink-0 px-3 py-1 text-sm'
          >
            {delivery.status === 'active'
              ? '流水线进行中'
              : delivery.status === 'blocked'
                ? '已阻塞'
                : '已完成'}
          </Badge>
        </div>
      </Header>

      <div className='mx-auto max-w-4xl space-y-6 p-6'>
        {/* 阶段推进 */}
        <Card>
          <CardHeader className='pb-4'>
            <div className='flex items-center justify-between'>
              <CardTitle className='text-sm font-medium'>阶段推进</CardTitle>
              <span className='text-xs text-muted-foreground'>
                {doneCount} / {STAGES.length} 阶段完成 · 失败回环{' '}
                {delivery.fail_count} 次
              </span>
            </div>
          </CardHeader>
          <CardContent>
            <ol className='flex items-start'>
              {STAGES.map((s, i) => {
                const state = stageState(s, currentIdx, i, delivery.pending_gate)
                const featured = state === 'current' || state === 'gate-waiting'
                const isLast = i === STAGES.length - 1
                return (
                  <li key={s} className='flex min-w-0 flex-1 flex-col'>
                    <div className='flex items-center'>
                      <div
                        className={
                          'flex size-7 shrink-0 items-center justify-center rounded-full border ' +
                          (state === 'done'
                            ? 'border-primary bg-primary text-primary-foreground'
                            : featured
                              ? 'border-primary text-foreground'
                              : 'border-input text-muted-foreground')
                        }
                      >
                        {GATES.has(s) && state !== 'done' ? (
                          <DoorOpen className='size-3.5' />
                        ) : (
                          <StageIcon state={state} />
                        )}
                      </div>
                      {!isLast && (
                        <div
                          className={
                            'h-px flex-1 ' +
                            (state === 'done' ? 'bg-primary' : 'bg-border')
                          }
                        />
                      )}
                    </div>
                    <div className='mt-2 pr-2'>
                      <div
                        className={
                          'text-xs font-medium leading-tight ' +
                          (state === 'pending'
                            ? 'text-muted-foreground'
                            : 'text-foreground')
                        }
                      >
                        {STAGE_META[s].label}
                        {GATES.has(s) && (
                          <span className='ml-1 font-normal text-muted-foreground'>
                            门禁
                          </span>
                        )}
                      </div>
                      <div className='mt-0.5 text-[11px] text-muted-foreground'>
                        {state === 'gate-waiting'
                          ? '等待你的审批'
                          : state === 'current'
                            ? '进行中'
                            : state === 'done'
                              ? '完成'
                              : ''}
                      </div>
                    </div>
                  </li>
                )
              })}
            </ol>

            {delivery.pending_gate && (
              <>
                <Separator className='my-5' />
                <div className='flex items-center justify-between gap-4'>
                  <div className='text-sm'>
                    <span className='font-medium'>
                      {STAGE_META[delivery.pending_gate as StageName].label}
                    </span>
                    <span className='ml-2 text-muted-foreground'>
                      正在等待你的决定，流水线在此暂停
                    </span>
                  </div>
                  <Link
                    to='/deliveries/$id/gate'
                    params={{ id: deliveryId }}
                    className={buttonVariants({ size: 'lg' })}
                  >
                    去审批
                  </Link>
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* 阶段档案 */}
        <Card>
          <CardHeader>
            <CardTitle className='text-sm font-medium'>交付档案</CardTitle>
            <CardDescription>
              按时间线垂直展开，回环与门禁停留一目了然
            </CardDescription>
          </CardHeader>
          <CardContent>
            {STAGES.map((s, i) => {
              const state = stageState(s, currentIdx, i, delivery.pending_gate)
              const events = eventsOf(s)
              return (
                <div key={s}>
                  {i > 0 && <Separator className='my-4' />}
                  <div className='flex items-start justify-between gap-4'>
                    <div className='min-w-0'>
                      <div className='flex items-center gap-2'>
                        <StageIcon state={state} />
                        <span
                          className={
                            'text-sm font-medium ' +
                            (state === 'pending'
                              ? 'text-muted-foreground'
                              : 'text-foreground')
                          }
                        >
                          {STAGE_META[s].label}
                        </span>
                        {GATES.has(s) && (
                          <Badge variant='outline' className='px-1.5 text-[10px]'>
                            门禁
                          </Badge>
                        )}
                      </div>
                      {state !== 'pending' && (
                        <p className='mt-1.5 pl-6 text-xs leading-relaxed text-muted-foreground'>
                          {STAGE_META[s].hint}
                        </p>
                      )}
                      {events.length > 0 && (
                        <ul className='mt-2 space-y-1 pl-6 font-mono text-[11px] leading-relaxed text-muted-foreground'>
                          {events.map((e: TimelineEvent) => (
                            <li
                              key={e.id}
                              title={new Date(e.created_at).toLocaleString()}
                            >
                              {timeAgo(e.created_at)} ·{' '}
                              {EVENT_LABEL[e.event_type] ?? e.event_type}
                            </li>
                          ))}
                        </ul>
                      )}
                    </div>
                    <Badge
                      variant={
                        state === 'gate-waiting'
                          ? 'default'
                          : state === 'pending'
                            ? 'outline'
                            : 'secondary'
                      }
                      className='shrink-0'
                    >
                      {state === 'gate-waiting'
                        ? '等待审批'
                        : state === 'current'
                          ? '进行中'
                          : state === 'done'
                            ? '完成'
                            : '未开始'}
                    </Badge>
                  </div>
                </div>
              )
            })}
          </CardContent>
        </Card>
      </div>
    </>
  )
}
