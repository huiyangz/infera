import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import {
  Check,
  ChevronDown,
  ChevronLeft,
  ChevronUp,
  Plus,
  Undo2,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import {
  approveGate,
  getDelivery,
  getGate,
  rejectGate,
  type ApproveOptions,
} from '@/lib/infera-api'
import type {
  ChildSpec,
  Finding,
  GateReview,
  TaskSpec,
} from '@/lib/infera-types'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Header } from '@/components/layout/header'
import { LocalHandleButton } from '@/features/deliveries/local-handle-button'
import {
  gateHasLocalRole,
  useLocalNodes,
} from '@/features/deliveries/local-link'

/** 门禁类型 → 展示配置；未知门禁回退通用文案，不崩。 */
const GATE_META: Record<
  string,
  { title: string; approveHint: string; artifactLabel: string; showPR: boolean }
> = {
  spec_approval: {
    title: '规格审批',
    approveHint: '选定交付模式后批准；打回则 Spec Agent 重写',
    artifactLabel: 'Spec 内容',
    showPR: false,
  },
  design_approval: {
    title: '设计审批',
    approveHint: '批准后进入任务生成；打回则 Design Agent 重写',
    artifactLabel: '设计文档',
    showPR: false,
  },
  tasks_approval: {
    title: '任务审批',
    approveHint: '批准后 Coder Agent 按清单逐任务实现；打回则 Task Agent 重列',
    artifactLabel: '任务清单',
    showPR: false,
  },
  code_review: {
    title: '代码审查',
    approveHint:
      '基于两道 Agent 审查意见与 diff 裁定（意见只呈现不拦截）；批准后合入（PR 已就绪），打回则 Coder Agent 重做',
    artifactLabel: 'Reviewer 意见',
    showPR: true,
  },
}

/** R10 双道审查 → 展示配置；未知道名回退原文标题 */
const REVIEW_META: Record<string, { title: string; emptyHint: string }> = {
  spec_conformance: {
    title: '规格符合性审查',
    emptyHint: '未发现规格偏差',
  },
  code_quality: { title: '代码质量审查', emptyHint: '未发现质量问题' },
}

/** 严重度 → 中文标签 + 单色分级（DESIGN.md 无信号色：用 tonal step 而非红黄） */
const SEVERITY_META: Record<string, { label: string; className: string }> = {
  critical: {
    label: '严重',
    className: 'border-transparent bg-primary text-primary-foreground',
  },
  major: {
    label: '重要',
    className: 'border-transparent bg-secondary text-secondary-foreground',
  },
  minor: { label: '轻微', className: 'text-foreground' },
  info: { label: '提示', className: 'text-muted-foreground' },
}

/** 单条审查意见：严重度 + 关联任务序号 + 结论 + 证据引用（有 PR 时可跳转查看） */
function FindingRow({ f, prUrl }: { f: Finding; prUrl?: string }) {
  const sev = SEVERITY_META[f.severity] ?? SEVERITY_META.info
  const evidenceClass =
    'font-mono text-xs text-muted-foreground underline decoration-dotted underline-offset-4 hover:text-foreground'
  return (
    <li className='space-y-1 border-b pb-3 last:border-b-0 last:pb-0'>
      <div className='flex flex-wrap items-center gap-2'>
        <Badge variant='outline' className={sev.className}>
          {sev.label}
        </Badge>
        {f.task_index > 0 && (
          <Badge variant='outline' className='font-mono text-muted-foreground'>
            任务 #{f.task_index}
          </Badge>
        )}
      </div>
      <p className='text-sm leading-relaxed'>{f.message}</p>
      {f.evidence &&
        (prUrl ? (
          <a
            href={prUrl}
            target='_blank'
            rel='noreferrer'
            title='在 PR 中查看证据'
            className={evidenceClass}
          >
            {f.evidence}
          </a>
        ) : (
          <span className='font-mono text-xs text-muted-foreground'>
            {f.evidence}
          </span>
        ))}
    </li>
  )
}

/** 一道审查意见卡片：findings 列表（严重度 + 任务序号 + 证据跳转）+ 原始输出折叠 */
function ReviewCard({ review, prUrl }: { review: GateReview; prUrl?: string }) {
  const meta = REVIEW_META[review.review] ?? {
    title: review.review,
    emptyHint: '无意见',
  }
  const findings = review.findings ?? []
  return (
    <div className='rounded-lg border p-4'>
      <div className='mb-3 flex items-center justify-between gap-2'>
        <h3 className='text-sm font-medium'>{meta.title}</h3>
        {!review.present ? (
          <span className='text-xs text-muted-foreground'>
            未产出（本机交互占位）
          </span>
        ) : findings.length === 0 ? (
          <span className='text-xs text-muted-foreground'>
            {meta.emptyHint}
          </span>
        ) : (
          <span className='text-xs text-muted-foreground'>
            {findings.length} 条意见
          </span>
        )}
      </div>
      {review.present && review.task_based && (
        <p className='mb-2 text-xs text-muted-foreground'>按任务清单逐项核验</p>
      )}
      {review.present && findings.length > 0 && (
        <ul className='space-y-3'>
          {findings.map((f, i) => (
            <FindingRow key={i} f={f} prUrl={prUrl} />
          ))}
        </ul>
      )}
      {review.present && review.raw && (
        <details className='mt-3'>
          <summary className='cursor-pointer text-xs text-muted-foreground select-none'>
            原始输出
          </summary>
          <pre className='mt-2 max-h-60 overflow-auto rounded-lg border bg-muted/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap'>
            {review.raw}
          </pre>
        </details>
      )}
    </div>
  )
}

/** 复杂度（交付模式）选项：spec 审批门裁定 */
const COMPLEXITY_OPTIONS: {
  value: 'small' | 'large'
  label: string
  hint: string
}[] = [
  {
    value: 'small',
    label: '小任务',
    hint: '规格确认后直达测试生成与实现（7 阶段）',
  },
  {
    value: 'large',
    label: '大任务',
    hint: '先做设计与任务拆解、逐门审批后实现（11 阶段）',
  },
]

/** 复杂度 segmented 控件：选中项高亮，点击即改判 */
function ComplexityPicker({
  value,
  onChange,
}: {
  value: 'small' | 'large'
  onChange: (v: 'small' | 'large') => void
}) {
  return (
    <div className='inline-flex items-center rounded-lg border bg-muted/50 p-0.5'>
      {COMPLEXITY_OPTIONS.map((o) => (
        <button
          key={o.value}
          type='button'
          onClick={() => onChange(o.value)}
          className={
            'rounded-md px-4 py-1.5 text-sm font-medium transition-colors ' +
            (value === o.value
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground')
          }
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function GatePage({ deliveryId }: { deliveryId: string }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['gate', deliveryId],
    queryFn: () => getGate(deliveryId),
  })
  // 本机交互通道（R4）：code_review 门禁有 local 绑定审查角色时给本机预审入口
  const { data: detail } = useQuery({
    queryKey: ['delivery', deliveryId],
    queryFn: () => getDelivery(deliveryId),
  })
  const localNodes = useLocalNodes(detail?.delivery.project_id)
  const [reason, setReason] = useState('')
  // spec_approval：交付模式（AI 建议预选，可改判）
  const [complexity, setComplexity] = useState<'small' | 'large' | null>(null)
  // design_approval：拆分方案行（AI 建议预填，可增删改）
  const [rows, setRows] = useState<ChildSpec[] | null>(null)
  // tasks_approval：任务清单行（引擎清单预填，可增删改/调序；null = 未编辑）
  const [taskRows, setTaskRows] = useState<TaskSpec[] | null>(null)

  const back = () =>
    navigate({ to: '/deliveries/$id', params: { id: deliveryId } })

  // 审批/打回后流水线会推进：只失效本交付与所属项目的相关 query（列表统计跟着变）
  const invalidateAfterDecision = () => {
    qc.invalidateQueries({ queryKey: ['delivery', deliveryId] })
    qc.invalidateQueries({ queryKey: ['gate', deliveryId] })
    const projectId = detail?.delivery.project_id
    if (projectId) {
      qc.invalidateQueries({ queryKey: ['project', projectId] })
      qc.invalidateQueries({ queryKey: ['project-deliveries', projectId] })
    } else {
      qc.invalidateQueries({ queryKey: ['project-deliveries'] })
    }
    qc.invalidateQueries({ queryKey: ['projects'] })
  }

  const approve = useMutation({
    mutationFn: (opts?: ApproveOptions) => approveGate(deliveryId, opts),
    onSuccess: (_d, opts) => {
      invalidateAfterDecision()
      if (opts?.split?.length)
        toast.success(`已拆分为 ${opts.split.length} 个子任务，流水线调度中`)
      else if (opts?.tasks?.length)
        toast.success(`已按 ${opts.tasks.length} 项任务清单开始实现`)
      else toast.success('已批准，流水线继续')
      back()
    },
    onError: (e: Error) => toast.error(e.message),
  })
  const reject = useMutation({
    mutationFn: () => rejectGate(deliveryId, reason),
    onSuccess: () => {
      invalidateAfterDecision()
      toast.success('已打回，Agent 将重新处理')
      back()
    },
    onError: (e: Error) => toast.error(e.message),
  })

  if (isLoading || !data)
    return (
      <>
        <Header fixed>
          <Skeleton className='h-4 w-40' />
        </Header>
        <div className='mx-auto max-w-3xl p-6'>
          <Skeleton className='h-64 w-full rounded-xl' />
        </div>
      </>
    )

  const meta = GATE_META[data.gate] ?? {
    title: '审批',
    approveHint: '批准后流水线继续',
    artifactLabel: '产出内容',
    showPR: false,
  }

  const isSpec = data.gate === 'spec_approval'
  const isDesign = data.gate === 'design_approval'
  const isTasks = data.gate === 'tasks_approval'
  const isReview = data.gate === 'code_review'
  // code_review 且审查角色（预审/双道）有 local 绑定：本机预审入口
  const localReview = gateHasLocalRole(data.gate, localNodes)
  // AI 复杂度建议：'' = 无建议，按 small 预选
  const suggestion = data.complexity_suggestion === 'large' ? 'large' : 'small'
  const picked = complexity ?? suggestion
  const overridden = complexity !== null && complexity !== suggestion
  // 懒播种：首次渲染用 AI 建议（或空）填充，之后完全由用户编辑
  const splitRows = rows ?? data.split_plan ?? []
  const setSplitRows = (next: ChildSpec[]) => setRows(next)
  const splitValid =
    splitRows.length > 0 && splitRows.every((r) => r.title.trim().length > 0)
  // 任务清单：未编辑（null）时跟随引擎清单展示
  const tasks = taskRows ?? data.tasks ?? []
  const tasksDirty = taskRows !== null
  const setTasks = (next: TaskSpec[]) => setTaskRows(next)
  const moveTask = (i: number, dir: -1 | 1) => {
    const j = i + dir
    if (j < 0 || j >= tasks.length) return
    const next = [...tasks]
    const moved = next[i]
    next[i] = next[j]
    next[j] = moved
    setTasks(next)
  }
  const tasksValid = tasks.every((t) => t.title.trim().length > 0)
  // 编辑过且清单非空 → 批准时携带覆盖；清空 = 普通批准
  const tasksOverride =
    tasksDirty && tasks.length > 0 && tasksValid ? tasks : undefined

  return (
    <>
      <Header fixed>
        <div className='flex w-full items-center gap-1 text-sm text-muted-foreground'>
          <ChevronLeft className='size-4' />
          <Link
            to='/deliveries/$id'
            params={{ id: deliveryId }}
            className='hover:text-foreground'
          >
            返回详情
          </Link>
          <span>/</span>
          <span className='text-foreground'>{meta.title}</span>
        </div>
      </Header>

      {/* 代码审查门加宽：两道审查意见与 diff 并列需要横向空间 */}
      <div className={`mx-auto p-6 ${isReview ? 'max-w-6xl' : 'max-w-3xl'}`}>
        <Card>
          <CardHeader>
            <CardTitle>{meta.title}</CardTitle>
            <CardDescription>{meta.approveHint}</CardDescription>
          </CardHeader>
          <CardContent className='space-y-5'>
            {/* 任务门的原始产出（清单 JSON）由下方编辑器承载；解析失败时兜底展示 */}
            {(!isTasks || data.tasks == null) && (
              <div>
                <h3 className='mb-2 text-sm font-medium'>
                  {isTasks
                    ? `${meta.artifactLabel}（原始产出）`
                    : meta.artifactLabel}
                </h3>
                <pre className='min-h-24 rounded-lg border bg-muted/50 p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap'>
                  {data.agent_output?.output || '（无 Agent 产出）'}
                </pre>
              </div>
            )}

            {meta.showPR && data.pr_url && (
              <p className='text-sm'>
                PR：
                <a
                  className='underline underline-offset-4 hover:text-primary'
                  href={data.pr_url}
                  target='_blank'
                  rel='noreferrer'
                >
                  {data.pr_url}
                </a>
              </p>
            )}

            {/* 代码审查门（R10）：两道 Agent 审查意见与代码 diff 并列——人审审查意见 */}
            {isReview && (
              <div className='grid gap-4 xl:grid-cols-2'>
                <div className='space-y-4'>
                  {(data.reviews ?? []).map((r) => (
                    <ReviewCard
                      key={r.review}
                      review={r}
                      prUrl={data.pr_url || undefined}
                    />
                  ))}
                  {!(data.reviews ?? []).length && (
                    <p className='text-sm text-muted-foreground'>
                      （无审查意见产出）
                    </p>
                  )}
                </div>
                <div>
                  <h3 className='mb-2 text-sm font-medium'>代码 diff</h3>
                  <pre className='max-h-[36rem] overflow-auto rounded-lg border bg-muted/50 p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap'>
                    {data.diff || '（无 diff）'}
                  </pre>
                </div>
              </div>
            )}

            {/* 规格门：交付模式裁定（small 直达 / large 走 SDD） */}
            {isSpec && (
              <div className='space-y-2'>
                <div className='flex items-center justify-between'>
                  <h3 className='text-sm font-medium'>交付模式</h3>
                  {data.complexity_suggestion ? (
                    <span className='text-xs text-muted-foreground'>
                      AI 建议：{suggestion === 'large' ? '大任务' : '小任务'}
                      {overridden && '（已改判）'}
                    </span>
                  ) : (
                    <span className='text-xs text-muted-foreground'>
                      AI 未给出建议，默认小任务
                    </span>
                  )}
                </div>
                <ComplexityPicker value={picked} onChange={setComplexity} />
                <p className='text-xs text-muted-foreground'>
                  {COMPLEXITY_OPTIONS.find((o) => o.value === picked)?.hint}
                </p>
              </div>
            )}

            {/* 设计门：拆分执行（可选，编辑器自规格门迁入） */}
            {isDesign && (
              <div className='space-y-2'>
                <div className='flex items-center justify-between'>
                  <h3 className='text-sm font-medium'>拆分执行（可选）</h3>
                  {data.split_plan?.length ? (
                    <span className='text-xs text-muted-foreground'>
                      AI 建议已预填，可清空
                    </span>
                  ) : null}
                </div>
                {splitRows.map((r, i) => (
                  <div key={i} className='flex items-center gap-2'>
                    <Input
                      className='w-40 shrink-0'
                      placeholder='子任务标题'
                      value={r.title}
                      onChange={(e) =>
                        setSplitRows(
                          splitRows.map((row, j) =>
                            j === i ? { ...row, title: e.target.value } : row
                          )
                        )
                      }
                    />
                    <Input
                      className='flex-1'
                      placeholder='补充描述…'
                      value={r.description}
                      onChange={(e) =>
                        setSplitRows(
                          splitRows.map((row, j) =>
                            j === i
                              ? { ...row, description: e.target.value }
                              : row
                          )
                        )
                      }
                    />
                    <Input
                      type='number'
                      min={1}
                      className='w-16 shrink-0 tabular-nums'
                      value={r.wave}
                      onChange={(e) =>
                        setSplitRows(
                          splitRows.map((row, j) =>
                            j === i
                              ? {
                                  ...row,
                                  wave: Math.max(
                                    1,
                                    Number(e.target.value) || 1
                                  ),
                                }
                              : row
                          )
                        )
                      }
                    />
                    <Button
                      variant='ghost'
                      size='icon'
                      className='shrink-0'
                      aria-label='删除该子任务'
                      onClick={() =>
                        setSplitRows(splitRows.filter((_, j) => j !== i))
                      }
                    >
                      <X />
                    </Button>
                  </div>
                ))}
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={() =>
                    setSplitRows([
                      ...splitRows,
                      { title: '', description: '', wave: 1 },
                    ])
                  }
                >
                  <Plus /> 添加子任务
                </Button>
              </div>
            )}

            {/* 任务门：清单编辑器（增删改 + 调序；批准时可覆盖引擎清单） */}
            {isTasks && (
              <div className='space-y-2'>
                <div className='flex items-center justify-between'>
                  <h3 className='text-sm font-medium'>任务清单</h3>
                  <span className='text-xs text-muted-foreground'>
                    {tasks.length
                      ? `共 ${tasks.length} 项，按序实现`
                      : '清单为空，可直接批准（Coder Agent 整体实现）'}
                  </span>
                </div>
                {tasks.map((t, i) => (
                  <div key={i} className='flex items-center gap-2'>
                    <span className='w-5 shrink-0 text-right font-mono text-xs text-muted-foreground'>
                      {i + 1}
                    </span>
                    <Input
                      className='w-56 shrink-0'
                      placeholder='任务标题'
                      value={t.title}
                      onChange={(e) =>
                        setTasks(
                          tasks.map((row, j) =>
                            j === i ? { ...row, title: e.target.value } : row
                          )
                        )
                      }
                    />
                    <Input
                      className='flex-1'
                      placeholder='任务详情…'
                      value={t.detail}
                      onChange={(e) =>
                        setTasks(
                          tasks.map((row, j) =>
                            j === i ? { ...row, detail: e.target.value } : row
                          )
                        )
                      }
                    />
                    <div className='flex shrink-0 items-center'>
                      <Button
                        variant='ghost'
                        size='icon'
                        aria-label='上移'
                        disabled={i === 0}
                        onClick={() => moveTask(i, -1)}
                      >
                        <ChevronUp />
                      </Button>
                      <Button
                        variant='ghost'
                        size='icon'
                        aria-label='下移'
                        disabled={i === tasks.length - 1}
                        onClick={() => moveTask(i, 1)}
                      >
                        <ChevronDown />
                      </Button>
                      <Button
                        variant='ghost'
                        size='icon'
                        aria-label='删除该任务'
                        onClick={() =>
                          setTasks(tasks.filter((_, j) => j !== i))
                        }
                      >
                        <X />
                      </Button>
                    </div>
                  </div>
                ))}
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={() =>
                    setTasks([...tasks, { title: '', detail: '' }])
                  }
                >
                  <Plus /> 添加任务
                </Button>
              </div>
            )}

            {/* 本机预审入口：审查角色 local 绑定时拉起本机 CLI 产出预审意见 */}
            {localReview && (
              <div className='flex flex-wrap items-center justify-between gap-4 rounded-lg border p-4'>
                <div className='text-sm'>
                  <span className='font-medium'>本机预审</span>
                  <span className='ml-2 text-muted-foreground'>
                    审查角色绑定本机——拉起本机 CLI 产出意见并交回后，再在此裁定
                  </span>
                </div>
                <LocalHandleButton deliveryId={deliveryId} />
              </div>
            )}

            <div className='flex items-center gap-2'>
              {isDesign ? (
                <>
                  <Button
                    size='lg'
                    disabled={approve.isPending}
                    onClick={() => approve.mutate(undefined)}
                  >
                    <Check /> 批准（不拆分）
                  </Button>
                  <Button
                    size='lg'
                    disabled={!splitValid || approve.isPending}
                    onClick={() => approve.mutate({ split: splitRows })}
                  >
                    <Check /> 批准并拆分
                  </Button>
                </>
              ) : isTasks ? (
                <Button
                  size='lg'
                  disabled={!tasksValid || approve.isPending}
                  onClick={() => approve.mutate({ tasks: tasksOverride })}
                >
                  <Check /> 批准，开始实现
                </Button>
              ) : (
                <Button
                  size='lg'
                  disabled={approve.isPending}
                  onClick={() =>
                    approve.mutate(isSpec ? { complexity: picked } : undefined)
                  }
                >
                  <Check /> 批准
                </Button>
              )}
              <Input
                className='flex-1'
                placeholder='打回理由…'
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
              <Button
                size='lg'
                variant='destructive'
                disabled={!reason.trim() || reject.isPending}
                onClick={() => reject.mutate()}
              >
                <Undo2 /> 打回
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </>
  )
}
