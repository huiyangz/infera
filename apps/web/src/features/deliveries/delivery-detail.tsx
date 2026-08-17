import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import {
  Check,
  ChevronLeft,
  Circle,
  Copy,
  DoorOpen,
  Loader2,
  Minus,
  X,
} from 'lucide-react'
import { getDelivery, mergeResume } from '@/lib/infera-api'
import {
  GATES,
  STAGES,
  STAGE_META,
  stageLabel,
  type DeliveryStatus,
  type StageName,
  type TimelineEvent,
} from '@/lib/infera-types'
import { dateTime, timeAgo } from '@/lib/time'
import { useDeliveryEvents } from '@/hooks/use-delivery-events'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Header } from '@/components/layout/header'
import { StatusBadge } from '@/components/status-badge'

type StageState =
  | 'done'
  | 'current'
  | 'gate-waiting'
  | 'pending'
  | 'failed'
  | 'skipped'

/** 后端真实事件词表（见 server engine）；未知事件回退原文。 */
const EVENT_LABEL: Record<string, string> = {
  delivery_created: '需求创建',
  workspace_ready: '工作区就绪',
  stage_started: '阶段开始',
  gate_pending: '进入门禁',
  gate_approved: '审批通过',
  gate_rejected: '审批打回',
  test_failed: '测试失败',
  stage_failed: '阶段失败',
  delivery_completed: '交付完成',
  delivery_blocked: '流水线阻塞',
  persist_done: '产出已推送',
  pr_failed: 'PR 开具失败',
  persist_failed: '产出持久化失败',
  split: '需求拆分',
  merge_done: '子需求已合并',
  merge_conflict: '合并冲突',
  merge_queued: '合并排队中',
  merge_skipped: '跳过合并',
  merge_resumed: '合并已恢复',
  merge_failed: '合并失败',
}

/**
 * 阶段状态需结合 delivery 终态判定：
 * completed → 全部 done；blocked → 当前阶段 failed（无 spinner），之前 done，之后 pending。
 */
function stageState(
  stage: StageName,
  currentIdx: number,
  idx: number,
  pendingGate: string | null,
  status: DeliveryStatus,
  splitMode = false
): StageState {
  // 拆分执行：父的测试生成不跑（子需求各自生成、父合并后统一跑单元测试）
  if (splitMode && stage === 'test_gen') return 'skipped'
  if (status === 'completed') return 'done'
  if (status === 'blocked') {
    if (idx < currentIdx) return 'done'
    if (idx === currentIdx) return 'failed'
    return 'pending'
  }
  if (stage === pendingGate) return 'gate-waiting'
  if (idx < currentIdx) return 'done'
  if (idx === currentIdx) return 'current'
  return 'pending'
}

function StageIcon({ state, idle }: { state: StageState; idle?: boolean }) {
  if (state === 'done') return <Check className='size-3.5' strokeWidth={2.5} />
  if (state === 'skipped') return <Minus className='size-3.5' strokeWidth={2.5} />
  if (state === 'failed') return <X className='size-3.5' strokeWidth={2.5} />
  if (state === 'current' || state === 'gate-waiting')
    return idle ? (
      <Circle className='size-3.5' />
    ) : (
      <Loader2 className='size-3.5 animate-spin' />
    )
  return <Circle className='size-3.5' />
}

const STAGE_STATUS_TEXT: Record<StageState, string> = {
  done: '完成',
  current: '进行中',
  'gate-waiting': '等待你的审批',
  'skipped': '跳过',
  pending: '',
  failed: '已阻塞',
}

export function DeliveryDetail({
  deliveryId,
  embedded = false,
}: {
  deliveryId: string
  embedded?: boolean
}) {
  useDeliveryEvents(deliveryId)
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['delivery', deliveryId],
    queryFn: () => getDelivery(deliveryId),
  })
  const resume = useMutation({
    mutationFn: () => mergeResume(deliveryId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['delivery', deliveryId] })
      toast.success('合并已恢复，流水线继续')
    },
    onError: (e: Error) => toast.error(e.message),
  })

  if (isLoading || !data)
    return (
      <div
        className={
          embedded
            ? 'w-full space-y-6 px-8 pb-8 pt-4'
            : 'mx-auto max-w-4xl space-y-6 p-6'
        }
      >
        <Skeleton className='h-36 w-full rounded-xl' />
        <Skeleton className='h-72 w-full rounded-xl' />
      </div>
    )

  const { delivery, timeline, artifacts } = data
  const children = data.children ?? []
  // 拆分父停在 code_gen：语义是「等子需求跑完再合并」，不是真的在写代码
  const splitWaiting =
    delivery.split_mode &&
    delivery.current_stage === 'code_gen' &&
    !delivery.pending_gate
  const kidsDone = children.filter((c) => c.status === 'completed').length
  // 最新一次合并冲突事件里给人工的 git 指令
  const conflictInstructions = [...timeline]
    .reverse()
    .find((e) => e.event_type === 'merge_conflict')
    ?.payload as
    | { instructions?: string; branches?: string[] }
    | undefined
  const currentIdx = STAGES.indexOf(delivery.current_stage as StageName)
  const stateOf = (s: StageName, i: number) =>
    stageState(
      s,
      currentIdx,
      i,
      delivery.pending_gate,
      delivery.status,
      delivery.split_mode,
    )
  const eventsOf = (s: StageName) => timeline.filter((e) => e.stage === s)
  const doneCount = STAGES.filter((s, i) => stateOf(s, i) === 'done').length
  // 最新一次实现产出的真实 git diff（从新往旧找第一条）
  const latestDiff = [...artifacts]
    .reverse()
    .find((a) => a.stage === 'code_review' && a.kind === 'diff')

  const titleBlock = (
    <div className='flex w-full items-start justify-between gap-4'>
      <div className='min-w-0'>
        <h1 className='truncate text-lg font-semibold tracking-[-0.2px]'>
          {delivery.title}
        </h1>
        <p className='mt-1 max-w-2xl text-sm text-muted-foreground'>
          {delivery.description || '（无补充描述）'}
        </p>
      </div>
      <StatusBadge
        status={delivery.status}
        className='mt-1 shrink-0 px-3 py-1 text-sm'
      />
    </div>
  )

  return (
    <>
      {embedded ? (
        <div className='border-b px-8 pb-3 pt-4'>{titleBlock}</div>
      ) : (
        <Header fixed>
          <div className='flex w-full items-start justify-between gap-4'>
            <div className='min-w-0'>
              <div className='flex items-center gap-1 text-sm text-muted-foreground'>
                <ChevronLeft className='size-4' />
                <Link
                  to='/projects/$id'
                  params={{ id: delivery.project_id }}
                  search={{ d: deliveryId }}
                  className='hover:text-foreground'
                >
                  返回项目
                </Link>
              </div>
              <h1 className='mt-1 truncate text-lg font-semibold tracking-[-0.2px]'>
                {delivery.title}
              </h1>
              <p className='mt-1 max-w-2xl text-sm text-muted-foreground'>
                {delivery.description || '（无补充描述）'}
              </p>
            </div>
            <StatusBadge
              status={delivery.status}
              className='mt-1 shrink-0 px-3 py-1 text-sm'
            />
          </div>
        </Header>
      )}

      <div
        className={
          embedded
            ? 'w-full space-y-6 px-8 pb-8 pt-4'
            : 'mx-auto max-w-4xl space-y-6 p-6'
        }
      >
        {/* 合并冲突横幅（拆分父专属） */}
        {delivery.merge_state === 'conflict' && (
          <Card className={embedded ? 'gap-3 py-4' : undefined}>
            <CardHeader className='pb-0'>
              <CardTitle className='text-sm font-medium'>合并冲突</CardTitle>
              <CardDescription>
                子需求分支与父分支冲突，需人工解决后推送，再点「继续」恢复流水线
              </CardDescription>
            </CardHeader>
            <CardContent className='space-y-3'>
              {conflictInstructions?.instructions && (
                <div className='relative'>
                  <pre className='max-h-64 overflow-auto rounded-lg border bg-muted/50 p-3 pe-12 font-mono text-[11px] leading-relaxed whitespace-pre-wrap'>
                    {conflictInstructions.instructions}
                  </pre>
                  <Button
                    variant='ghost'
                    size='icon'
                    className='absolute end-1.5 top-1.5'
                    aria-label='复制指令'
                    onClick={() => {
                      navigator.clipboard
                        .writeText(conflictInstructions.instructions ?? '')
                        .then(() => toast.success('已复制'))
                        .catch(() => toast.error('复制失败'))
                    }}
                  >
                    <Copy />
                  </Button>
                </div>
              )}
              <Button
                size='lg'
                disabled={resume.isPending}
                onClick={() => resume.mutate()}
              >
                {resume.isPending ? '恢复中…' : '合并已推送，继续'}
              </Button>
            </CardContent>
          </Card>
        )}

        {/* 阶段推进 */}
        <Card className={embedded ? 'gap-3 py-4' : undefined}>
          <CardHeader className='pb-0'>
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
                const state = stateOf(s, i)
                const active = state === 'current' || state === 'gate-waiting'
                const filled = state === 'done' || state === 'failed'
                const isLast = i === STAGES.length - 1
                const idle =
                  splitWaiting && s === 'code_gen' && state === 'current'
                return (
                  <li key={s} className='flex min-w-0 flex-1 flex-col'>
                    <div className='flex items-center'>
                      <div
                        className={
                          'flex size-7 shrink-0 items-center justify-center rounded-full border ' +
                          (filled
                            ? 'border-primary bg-primary text-primary-foreground'
                            : active
                              ? 'border-primary text-foreground'
                              : 'border-input text-muted-foreground')
                        }
                      >
                        {GATES.has(s) && !filled ? (
                          <DoorOpen className='size-3.5' />
                        ) : (
                          <StageIcon state={state} idle={idle} />
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
                          'text-xs leading-tight font-medium ' +
                          (state === 'pending'
                            ? 'text-muted-foreground'
                            : 'text-foreground')
                        }
                      >
                        {stageLabel(s)}
                        {GATES.has(s) && (
                          <span className='ml-1 font-normal text-muted-foreground'>
                            门禁
                          </span>
                        )}
                      </div>
                      <div className='mt-0.5 text-[11px] text-muted-foreground'>
                        {idle
                          ? delivery.merge_state === 'conflict'
                            ? '合并冲突'
                            : `等待子需求 ${kidsDone}/${children.length}`
                          : STAGE_STATUS_TEXT[state]}
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
                      {stageLabel(delivery.pending_gate)}
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

        {/* 子需求清单（拆分父专属） */}
        {delivery.split_mode && children.length > 0 && (
          <Card className={embedded ? 'gap-3 py-4' : undefined}>
            <CardHeader className='pb-0'>
              <div className='flex items-center justify-between'>
                <CardTitle className='text-sm font-medium'>子需求清单</CardTitle>
                <span className='text-xs text-muted-foreground'>
                  {kidsDone} / {children.length} 完成
                </span>
              </div>
            </CardHeader>
            <CardContent>
              {children.map((c, i) => (
                <div key={c.id}>
                  {i > 0 && <Separator className='my-3' />}
                  <Link
                    to='/projects/$id'
                    params={{ id: delivery.project_id }}
                    search={{ d: c.id }}
                    className='flex items-center justify-between gap-3 rounded-md px-1 py-1 transition-colors hover:bg-accent/50'
                  >
                    <span className='flex min-w-0 items-center gap-2'>
                      <Badge variant='outline' className='shrink-0 px-1.5 text-[10px]'>
                        批次 {c.wave || 1}
                      </Badge>
                      <span className='truncate text-sm'>{c.title}</span>
                    </span>
                    <span className='flex shrink-0 items-center gap-2'>
                      <span className='text-xs text-muted-foreground'>
                        {stageLabel(c.current_stage)}
                      </span>
                      <StatusBadge status={c.status} />
                    </span>
                  </Link>
                </div>
              ))}
            </CardContent>
          </Card>
        )}

        {/* 阶段档案 */}
        <Card className={embedded ? 'gap-3 py-4' : undefined}>
          <CardHeader className='pb-0'>
            <CardTitle className='text-sm font-medium'>交付档案</CardTitle>
            <CardDescription>
              按时间线垂直展开，回环与门禁停留一目了然
            </CardDescription>
          </CardHeader>
          <CardContent>
            {STAGES.map((s, i) => {
              const state = stateOf(s, i)
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
                          {stageLabel(s)}
                        </span>
                        {GATES.has(s) && (
                          <Badge
                            variant='outline'
                            className='px-1.5 text-[10px]'
                          >
                            门禁
                          </Badge>
                        )}
                      </div>
                      {state === 'skipped' ? (
                        <p className='mt-1.5 pl-6 text-xs leading-relaxed text-muted-foreground'>
                          拆分执行：子需求各自生成测试，父在合并后统一跑单元测试
                        </p>
                      ) : (
                        state !== 'pending' && (
                          <p className='mt-1.5 pl-6 text-xs leading-relaxed text-muted-foreground'>
                            {STAGE_META[s]?.hint}
                          </p>
                        )
                      )}
                      {events.length > 0 && (
                        <ul className='mt-2 space-y-1 pl-6 font-mono text-[11px] leading-relaxed text-muted-foreground'>
                          {events.map((e: TimelineEvent) => (
                            <li key={e.id} title={dateTime(e.created_at)}>
                              {timeAgo(e.created_at)} ·{' '}
                              {EVENT_LABEL[e.event_type] ?? e.event_type}
                            </li>
                          ))}
                        </ul>
                      )}
                      {s === 'code_gen' && latestDiff && (
                        <Collapsible className='mt-2 pl-6'>
                          <CollapsibleTrigger className='text-[11px] text-muted-foreground underline underline-offset-4 hover:text-foreground'>
                            查看代码变更 diff
                          </CollapsibleTrigger>
                          <CollapsibleContent>
                            <pre className='mt-2 max-h-72 overflow-auto rounded-lg border bg-muted/50 p-3 font-mono text-[11px] leading-relaxed'>
                              {latestDiff.content}
                            </pre>
                          </CollapsibleContent>
                        </Collapsible>
                      )}
                    </div>
                    <Badge
                      variant={
                        state === 'gate-waiting' || state === 'failed'
                          ? 'default'
                          : state === 'pending' || state === 'skipped'
                            ? 'outline'
                            : 'secondary'
                      }
                      className='shrink-0'
                    >
                      {state === 'gate-waiting'
                        ? '等待审批'
                        : state === 'skipped'
                          ? '跳过'
                          : state === 'current'
                            ? '进行中'
                            : state === 'failed'
                              ? '已阻塞'
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
