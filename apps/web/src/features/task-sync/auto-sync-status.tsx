import { useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { timeAgo } from '@/lib/time'
import { Button } from '@/components/ui/button'
import {
  getTaskSyncStatus,
  triggerTaskSync,
  type TaskSyncResult,
} from './api'

/**
 * 同步影响的视图查询 key（新一轮同步完成后整体失效，列表自动出新数据）：
 * 项目侧（projects / project 详情 / 交付列表 / 任务分组）与需求侧
 * （requirements / requirement 详情 / 待决策）。前缀失效：['requirement']
 * 覆盖 ['requirement', id] 等详情查询。
 */
const SYNCED_DATA_KEYS = [
  ['projects'],
  ['project'],
  ['project-deliveries'],
  ['project-task-groups'],
  ['requirements'],
  ['requirement'],
  ['pending-decisions'],
]

/** 成功反馈文案：导入计数，有跳过才提跳过（smoke 单 / 无项目单等） */
function summary(r: TaskSyncResult): string {
  let s = `同步完成：导入 ${r.projects_imported} 个项目 / ${r.issues_imported} 条任务`
  if (r.issues_skipped > 0) s += `，跳过 ${r.issues_skipped} 条`
  return s
}

/**
 * 「自动同步」状态条（INFERA-170）：同步由服务端调度全自动执行，本组件
 * 只做两件事——
 * 1. 轮询 GET /api/task-sync/status 展示「自动同步 · 上次同步 …」；
 *    status=error 时状态行变失败并透出 error 字段原文（可见失败提示）。
 * 2. 发现 lastSyncAt 变化（服务端完成了一轮新同步）即整体失效
 *    SYNCED_DATA_KEYS，requirements / projects 等视图随同步自动刷新；
 *    手动「立即同步」POST 成功后失效状态查询，走同一刷新路径。
 * 不点任何按钮，任务数据自动到位；「立即同步」仅为低调补充入口。
 */
export function AutoSyncStatus({ pollMs = 15_000 }: { pollMs?: number }) {
  const qc = useQueryClient()
  const status = useQuery({
    queryKey: ['task-sync-status'],
    queryFn: getTaskSyncStatus,
    retry: false, // 503 未装配等如实呈现，不重试掩盖
    // running 收紧轮询让「进行中 → 完成」秒级可感知；空闲按 pollMs 常规
    // 轮询——服务端调度器自动同步的新完成轮次靠它被发现
    refetchInterval: (query) =>
      query.state.data?.status === 'running' ? Math.min(2_000, pollMs) : pollMs,
  })
  const st = status.data
  const running = st?.status === 'running'

  // lastSyncAt 变化 = 服务端完成了一轮新同步（含手动触发的那轮）→ 刷新数据视图。
  // 首次观测只记基线，不触发失效。
  const prevSyncAt = useRef<string | null | undefined>(undefined)
  useEffect(() => {
    if (!st) return
    const prev = prevSyncAt.current
    prevSyncAt.current = st.lastSyncAt
    if (prev === undefined || prev === st.lastSyncAt) return
    for (const key of SYNCED_DATA_KEYS) qc.invalidateQueries({ queryKey: key })
  }, [qc, st])

  const sync = useMutation({
    mutationFn: triggerTaskSync,
    onSuccess: (r) => {
      toast.success(summary(r))
      // 状态查询重拉后 lastSyncAt 前移，由上面的 effect 统一刷新数据视图
      qc.invalidateQueries({ queryKey: ['task-sync-status'] })
    },
    // 409 运行中 / 502 上游失败 / 503 未装配：文案由后端给足，直接透传
    onError: (e: Error) => toast.error(e.message),
  })

  const busy = running || sync.isPending
  const hint = running
    ? '自动同步 · 同步中…'
    : st?.status === 'error'
      ? '自动同步 · 上次同步失败'
      : st?.lastSyncAt
        ? `自动同步 · 上次同步 ${timeAgo(st.lastSyncAt)}`
        : st
          ? '自动同步 · 从未同步'
          : null

  // 状态查询失败（未装配 503 / 网络异常）不渲染：那是装配态问题而非同步失败，
  // 常驻侧栏只会制造噪音；同步失败的可视提示由 status=error 承载
  if (!st) return null

  return (
    <div
      data-slot='auto-sync-status'
      className='group-data-[collapsible=icon]:hidden px-2 pb-1'
    >
      <div className='flex items-center justify-between gap-2'>
        <span
          className='flex min-w-0 items-center gap-1.5 text-xs tabular-nums text-muted-foreground'
          data-status={st.status}
        >
          {busy ? (
            <Loader2 className='size-3 shrink-0 animate-spin' />
          ) : (
            <RefreshCw className='size-3 shrink-0' />
          )}
          <span className='truncate'>{hint}</span>
        </span>
        <Button
          variant='ghost'
          size='icon'
          className='size-6 shrink-0'
          aria-label='立即同步'
          disabled={busy}
          onClick={() => sync.mutate()}
        >
          <RefreshCw className='size-3' />
        </Button>
      </div>
      {st.status === 'error' && st.error && (
        <p className='mt-1 truncate text-xs text-destructive' title={st.error}>
          {st.error}
        </p>
      )}
    </div>
  )
}
