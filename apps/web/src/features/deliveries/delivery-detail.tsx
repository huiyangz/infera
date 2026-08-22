import { useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  Check,
  ChevronLeft,
  Circle,
  Copy,
  DoorOpen,
  Loader2,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { getDelivery, mergeResume } from '@/lib/infera-api'
import {
  GATES,
  SPLIT_PARENT_SKIPPED,
  STAGE_META,
  stageLabel,
  stagesForDelivery,
  type Artifact,
  type DeliveryStatus,
  type StageName,
  type TaskSpec,
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
import { LocalHandleButton } from '@/features/deliveries/local-handle-button'
import {
  parkedAtLocalNode,
  useLocalNodes,
} from '@/features/deliveries/local-link'

type StageState =
  | 'done'
  | 'current'
  | 'gate-waiting'
  | 'pending'
  | 'failed'
  | 'skipped'

/** 后端真实事件词表（见 server engine）；未知事件回退原文。 */
const EVENT_LABEL: Record<string, string> = {
  delivery_created: '任务创建',
  workspace_ready: '工作区就绪',
  stage_started: '阶段开始',
  gate_pending: '进入门禁',
  gate_approved: '审批通过',
  gate_rejected: '审批打回',
  complexity_set: '复杂度已裁定',
  task_done: '任务完成',
  tasks_overridden: '任务清单已覆盖',
  wave_started: '批次启动',
  local_stage_pending: '待本机执行',
  test_failed: '测试失败',
  stage_failed: '阶段失败',
  delivery_completed: '交付完成',
  delivery_blocked: '流水线阻塞',
  persist_done: '产出已推送',
  review_findings: '门禁前置审查产出意见',
  pr_failed: 'PR 开具失败',
  persist_failed: '产出持久化失败',
  split: '任务拆分',
  merge_done: '子任务已合并',
  merge_conflict: '合并冲突',
  merge_queued: '合并排队中',
  merge_skipped: '跳过合并',
  merge_resumed: '合并已恢复',
  merge_failed: '合并失败',
}

/** 拆分父被跳过阶段的说明（阶段档案区展示） */
const SPLIT_SKIP_HINT: Record<string, string> = {
  tasks: '拆分执行：任务拆解由子任务各自完成，父不做任务拆解',
  tasks_approval: '拆分执行：任务清单由子任务各自审批，父直接合并子分支',
  test_gen: '拆分执行：子任务各自生成测试，父在合并后统一跑单元测试',
}

/**
 * 任务进度（large 模式 tasks→code_gen 逐任务实现）：
 * 清单取最新 kind=tasks artifact（引擎解析后的 JSON，覆盖后为新追加）；
 * 完成集合取 kind=task_done artifact（content=1-based 序号）；
 * 无清单 artifact 时从 task_done 事件回退拼装。无可展示进度返回 null。
 */
function taskProgress(
  artifacts: Artifact[],
  timeline: TimelineEvent[]
): { tasks: TaskSpec[]; done: Set<number> } | null {
  const done = new Set<number>()
  for (const a of artifacts) {
    if (a.kind === 'task_done') {
      const idx = Number.parseInt(a.content, 10)
      if (Number.isFinite(idx) && idx > 0) done.add(idx)
    }
  }
  const latest = [...artifacts].reverse().find((a) => a.kind === 'tasks')
  if (latest) {
    try {
      const tasks = JSON.parse(latest.content) as TaskSpec[]
      if (Array.isArray(tasks) && tasks.length > 0) return { tasks, done }
    } catch {
      // 坏 JSON：落到事件回退
    }
  }
  // 回退：无清单 artifact（老数据）时用 task_done 事件的 total + title 拼装
  const doneEvents = timeline.filter((e) => e.event_type === 'task_done')
  if (doneEvents.length === 0) return null
  let total = 0
  const byIndex = new Map<number, string>()
  for (const e of doneEvents) {
    const p = e.payload as {
      index?: number
      total?: number
      title?: string
    } | null
    if (typeof p?.total === 'number') total = Math.max(total, p.total)
    if (typeof p?.index === 'number' && p.title) byIndex.set(p.index, p.title)
    if (typeof p?.index === 'number') done.add(p.index)
  }
  if (total <= 0) return null
  const tasks: TaskSpec[] = []
  for (let i = 1; i <= total; i++)
    tasks.push({ title: byIndex.get(i) ?? `任务 ${i}`, detail: '' })
  return { tasks, done }
}

/**
 * 阶段状态需结合 delivery 终态判定：
 * completed → 全部 done；blocked → 当前阶段 failed（无 spinner），之前 done，之后 pending。
 * 拆分父的 tasks/tasks_approval/test_gen 恒为跳过态（任务与测试由子需求承担）。
 */
function stageState(
  stage: StageName,
  currentIdx: number,
  idx: number,
  pendingGate: string | null,
  status: DeliveryStatus,
  splitMode = false
): StageState {
  if (splitMode && SPLIT_PARENT_SKIPPED.has(stage)) return 'skipped'
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
  if (state === 'skipped')
    return <Check className='size-3.5' strokeWidth={2.5} />
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
  skipped: '跳过',
  pending: '',
  failed: '已阻塞',
}

/** 加载中/无数据时的稳定空引用：避免 useMemo 依赖每次 render 变化 */
const EMPTY_TIMELINE: TimelineEvent[] = []
const EMPTY_ARTIFACTS: Artifact[] = []

export function DeliveryDetail({ deliveryId }: { deliveryId: string }) {
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['delivery', deliveryId],
    queryFn: () => getDelivery(deliveryId),
  })
  // WS 订阅带上所属项目：事件失效只打本项目相关 query
  useDeliveryEvents(deliveryId, data?.delivery.project_id)
  // 本机交互通道（R4）：编排里 local 绑定的节点集合（加载中为 null）
  const localNodes = useLocalNodes(data?.delivery.project_id)
  const resume = useMutation({
    mutationFn: () => mergeResume(deliveryId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['delivery', deliveryId] })
      toast.success('合并已恢复，流水线继续')
    },
    onError: (e: Error) => toast.error(e.message),
  })

  // 派生数据集中在 memo 里：timeline/artifacts 的全量 reverse/filter
  // 只在数据变化时执行，不再每次 render 重算
  const timeline = data?.timeline ?? EMPTY_TIMELINE
  const artifacts = data?.artifacts ?? EMPTY_ARTIFACTS
  // 最新一次合并冲突事件里给人工的 git 指令
  const conflictInstructions = useMemo(
    () =>
      [...timeline]
        .reverse()
        .find((e) => e.event_type === 'merge_conflict')?.payload as
        | { instructions?: string; branches?: string[] }
        | undefined,
    [timeline]
  )
  const taskProgressData = useMemo(
    () => taskProgress(artifacts, timeline),
    [artifacts, timeline]
  )
  // code_review 门禁固化（persist）时落盘的真实 git diff（从新往旧取最近一条）
  const latestDiff = useMemo(
    () =>
      [...artifacts]
        .reverse()
        .find((a) => a.stage === 'code_review' && a.kind === 'diff'),
    [artifacts]
  )

  if (isLoading || !data)
    return (
      <div className='mx-auto max-w-4xl space-y-6 p-6'>
        <Skeleton className='h-36 w-full rounded-xl' />
        <Skeleton className='h-72 w-full rounded-xl' />
      </div>
    )

  const { delivery } = data
  const children = data.children ?? []
  // 停在本机绑定节点（local 停车）：展示「在本地处理此阶段」入口
  const parkedAtLocal = parkedAtLocalNode(delivery, localNodes)
  // 拆分父停在 code_gen：语义是「等子需求跑完再合并」，不是真的在写代码
  const splitWaiting =
    delivery.split_mode &&
    delivery.current_stage === 'code_gen' &&
    !delivery.pending_gate
  const kidsDone = children.filter((c) => c.status === 'completed').length
  // 阶段条按交付模式派生：small/老数据 7 阶段；large 全 11（拆分父含跳过态）
  const stages = stagesForDelivery(delivery)
  const currentIdx = stages.indexOf(delivery.current_stage as StageName)
  const stateOf = (s: StageName, i: number) =>
    stageState(
      s,
      currentIdx,
      i,
      delivery.pending_gate,
      delivery.status,
      delivery.split_mode
    )
  const eventsOf = (s: StageName) => timeline.filter((e) => e.stage === s)
  const doneCount = stages.filter((s, i) => stateOf(s, i) === 'done').length

  return (
    <>
      {/* 顶栏保持单行紧凑（h-16 固定高塞不下描述——描述面板归位到正文信息卡） */}
      <Header fixed>
        <div className='flex w-full min-w-0 items-center gap-2'>
          <div className='flex shrink-0 items-center gap-1 text-sm text-muted-foreground'>
            <ChevronLeft className='size-4' />
            <Link
              to='/projects/$id/tasks'
              params={{ id: delivery.project_id }}
              className='hover:text-foreground'
            >
              返回项目任务
            </Link>
          </div>
          <span className='shrink-0 text-sm text-muted-foreground'>/</span>
          <span
            className='min-w-0 truncate text-sm font-medium text-foreground'
            title={delivery.title}
          >
            {delivery.title}
          </span>
          <StatusBadge status={delivery.status} className='shrink-0' />
        </div>
      </Header>

      <div className='mx-auto w-full max-w-4xl space-y-6 p-6'>
        {/* 任务信息：标题全文 + 元信息 + 描述面板（换行保留，对齐需求详情的信息卡形态） */}
        <Card className='gap-4 py-5'>
          <CardHeader className='px-5'>
            <CardTitle className='text-base font-semibold tracking-[-0.2px]'>
              {delivery.title}
            </CardTitle>
            <CardDescription>
              阶段 {stageLabel(delivery.current_stage)} · 创建{' '}
              {dateTime(delivery.created_at)} · 更新{' '}
              {dateTime(delivery.updated_at)}
            </CardDescription>
          </CardHeader>
          <CardContent className='px-5'>
            <h3 className='mb-1 text-xs font-medium text-muted-foreground'>
              描述
            </h3>
            <p className='text-sm leading-relaxed whitespace-pre-wrap'>
              {delivery.description || '（无补充描述）'}
            </p>
          </CardContent>
        </Card>

        {/* 合并冲突横幅（拆分父专属） */}
        {delivery.merge_state === 'conflict' && (
          <Card>
            <CardHeader className='pb-0'>
              <CardTitle className='text-sm font-medium'>合并冲突</CardTitle>
              <CardDescription>
                子任务分支与父分支冲突，需人工解决后推送，再点「继续」恢复流水线
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
        <Card>
          <CardHeader className='pb-0'>
            <div className='flex items-center justify-between'>
              <CardTitle className='text-sm font-medium'>阶段推进</CardTitle>
              <span className='text-xs text-muted-foreground'>
                {doneCount} / {stages.length} 阶段完成 · 失败回环{' '}
                {delivery.fail_count} 次
              </span>
            </div>
          </CardHeader>
          <CardContent>
            <ol className='flex items-start'>
              {stages.map((s, i) => {
                const state = stateOf(s, i)
                const active = state === 'current' || state === 'gate-waiting'
                const filled = state === 'done' || state === 'failed'
                const isLast = i === stages.length - 1
                const idle =
                  splitWaiting && s === 'code_gen' && state === 'current'
                return (
                  <li key={s} className='flex min-w-0 flex-1 flex-col'>
                    <div className='flex items-center'>
                      <div
                        className={
                          'flex size-7 shrink-0 items-center justify-center rounded-full border ' +
                          (state === 'skipped'
                            ? 'border-dashed border-input text-muted-foreground'
                            : filled
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
                            (state === 'done'
                              ? 'bg-primary'
                              : state === 'skipped'
                                ? 'bg-input'
                                : 'bg-border')
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
                            : `等待子任务 ${kidsDone}/${children.length}`
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

            {/* 本机交互停车（local 绑定）：拉起本机 CLI 处理该阶段 */}
            {parkedAtLocal && (
              <>
                <Separator className='my-5' />
                <div className='flex items-center justify-between gap-4'>
                  <div className='text-sm'>
                    <span className='font-medium'>
                      {stageLabel(delivery.current_stage)}
                    </span>
                    <span className='ml-2 text-muted-foreground'>
                      停在本机交互（local 绑定）——在本地完成该阶段后经 MCP 交回
                    </span>
                  </div>
                  <LocalHandleButton deliveryId={deliveryId} />
                </div>
              </>
            )}
          </CardContent>
        </Card>

        {/* 任务进度（large 模式逐任务实现；task_done 持久推进） */}
        {taskProgressData && (
          <Card>
            <CardHeader className='pb-0'>
              <div className='flex items-center justify-between'>
                <CardTitle className='text-sm font-medium'>任务进度</CardTitle>
                <span className='text-xs text-muted-foreground'>
                  任务{' '}
                  {
                    [...taskProgressData.done].filter(
                      (i) => i <= taskProgressData.tasks.length
                    ).length
                  }{' '}
                  / {taskProgressData.tasks.length} 完成
                </span>
              </div>
            </CardHeader>
            <CardContent>
              <ol className='space-y-2.5'>
                {taskProgressData.tasks.map((t, i) => {
                  const done = taskProgressData.done.has(i + 1)
                  return (
                    <li key={i} className='flex items-center gap-2.5'>
                      <span
                        className={
                          'flex size-5 shrink-0 items-center justify-center rounded-full border ' +
                          (done
                            ? 'border-primary bg-primary text-primary-foreground'
                            : 'border-input text-muted-foreground')
                        }
                      >
                        {done ? (
                          <Check className='size-3' strokeWidth={2.5} />
                        ) : (
                          <Circle className='size-2.5' />
                        )}
                      </span>
                      <span
                        className={
                          'text-sm ' +
                          (done ? 'text-foreground' : 'text-muted-foreground')
                        }
                        title={t.detail || undefined}
                      >
                        {i + 1}. {t.title}
                      </span>
                    </li>
                  )
                })}
              </ol>
            </CardContent>
          </Card>
        )}

        {/* 子任务清单（拆分父专属） */}
        {delivery.split_mode && children.length > 0 && (
          <Card>
            <CardHeader className='pb-0'>
              <div className='flex items-center justify-between'>
                <CardTitle className='text-sm font-medium'>
                  子任务清单
                </CardTitle>
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
                    to='/deliveries/$id'
                    params={{ id: c.id }}
                    className='flex items-center justify-between gap-3 rounded-md px-1 py-1 transition-colors hover:bg-accent/50'
                  >
                    <span className='flex min-w-0 items-center gap-2'>
                      <Badge
                        variant='outline'
                        className='shrink-0 px-1.5 text-[10px]'
                      >
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
        <Card>
          <CardHeader className='pb-0'>
            <CardTitle className='text-sm font-medium'>交付档案</CardTitle>
            <CardDescription>
              按时间线垂直展开，回环与门禁停留一目了然
            </CardDescription>
          </CardHeader>
          <CardContent>
            {stages.map((s, i) => {
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
                          {SPLIT_SKIP_HINT[s] ??
                            '拆分执行：该阶段由子任务各自完成'}
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
