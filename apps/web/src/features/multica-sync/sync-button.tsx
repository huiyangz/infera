import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { timeAgo } from '@/lib/time'
import { Button } from '@/components/ui/button'
import {
  getMulticaSyncStatus,
  triggerMulticaSync,
  type MulticaSyncResult,
} from './api'

/** 成功反馈文案：导入计数，有跳过才提跳过（smoke 单 / 无项目单等） */
function summary(r: MulticaSyncResult): string {
  let s = `同步完成：导入 ${r.projects_imported} 个项目 / ${r.issues_imported} 条任务`
  if (r.issues_skipped > 0) s += `，跳过 ${r.issues_skipped} 条`
  return s
}

/**
 * 「从 Multica 同步」入口（INFERA-81 T04）：POST 触发一轮全量同步，
 * 成功反馈计数并失效 projects / project-deliveries 让列表刷新出新数据；
 * GET 状态承载进行中感知（running=true 收紧轮询直到完成）与
 * 「从未同步」（last=null——同步结果仅进程内存，服务重启即空）。
 */
export function MulticaSyncButton() {
  const qc = useQueryClient()
  const status = useQuery({
    queryKey: ['multica-sync-status'],
    queryFn: getMulticaSyncStatus,
    retry: false, // 503 未装配等如实交给按钮反馈，不重试掩盖
    // 运行中收紧轮询让「进行中 → 完成」可感知；空闲不轮询
    refetchInterval: (query) => (query.state.data?.running ? 2000 : false),
  })
  const running = status.data?.running ?? false
  const last = status.data?.last ?? null

  const sync = useMutation({
    mutationFn: triggerMulticaSync,
    onSuccess: (r) => {
      toast.success(summary(r))
      // 同步改变项目列表与各项目需求列表，按前缀整体失效重拉
      qc.invalidateQueries({ queryKey: ['projects'] })
      qc.invalidateQueries({ queryKey: ['project-deliveries'] })
      qc.invalidateQueries({ queryKey: ['multica-sync-status'] })
    },
    // 409 运行中 / 502 上游失败 / 503 未装配：文案由后端给足，直接透传
    onError: (e: Error) => toast.error(e.message),
  })

  const busy = running || sync.isPending
  const hint = running
    ? '同步进行中…'
    : last
      ? `上次同步 ${timeAgo(last.finished_at)} · 导入 ${last.projects_imported} 个项目 / ${last.issues_imported} 条任务`
      : '从未同步'

  return (
    <div className='flex items-center gap-2'>
      {status.isSuccess && (
        <span className='text-xs tabular-nums text-muted-foreground'>
          {hint}
        </span>
      )}
      <Button
        variant='outline'
        size='lg'
        disabled={busy}
        onClick={() => sync.mutate()}
      >
        {busy ? <Loader2 className='animate-spin' /> : <RefreshCw />}
        {busy ? '同步进行中…' : '从 Multica 同步'}
      </Button>
    </div>
  )
}
