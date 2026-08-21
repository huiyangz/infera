import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { SlidersHorizontal } from 'lucide-react'
import { toast } from 'sonner'
import { listProjects } from '@/lib/infera-api'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getMergePolicy, setMergePolicy } from './api'
import type { MergePolicyMode } from './types'

/** 三档合并策略（FR-6）：label 即按钮文案，hint 说明生效语义 */
const MODE_OPTIONS: {
  value: MergePolicyMode
  label: string
  hint: string
}[] = [
  {
    value: 'manual',
    label: '手动合并',
    hint: '合并卡出现后由人点击合并（默认）',
  },
  {
    value: 'auto_pass',
    label: 'PASS 自动',
    hint: 'Reviewer 结论为 PASS 即自动合并，不弹卡',
  },
  {
    value: 'threshold',
    label: '阈值混合',
    hint: 'diff 行数低于阈值自动合并，达到阈值弹出合并卡',
  },
]

const MODE_LABELS: Record<string, string> = Object.fromEntries(
  MODE_OPTIONS.map((o) => [o.value, o.label])
)

/**
 * 项目合并策略设置（FR-6）：策略按项目生效——先选项目，读取当前档位，
 * 三档单选 + 阈值输入（threshold 档必填正整数；manual/auto_pass 档不携带
 * 阈值，与服务端 flow.Validate 语义一致）。
 */
export function MergePolicySettings() {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [projectId, setProjectId] = useState<string>('')
  const [mode, setMode] = useState<MergePolicyMode | null>(null)
  const [threshold, setThreshold] = useState('0')

  const { data: projects } = useQuery({
    queryKey: ['projects'],
    queryFn: () => listProjects(false),
    enabled: open,
  })

  // 选中项目后读取当前策略；读取成功前不预选档位（避免闪烁错档）
  const { data: policy } = useQuery({
    queryKey: ['merge-policy', projectId],
    queryFn: () => getMergePolicy(projectId),
    enabled: open && !!projectId,
  })
  if (policy && mode === null) {
    setMode(policy.mode)
    setThreshold(String(policy.diff_line_threshold))
  }

  const thresholdNum = Number(threshold)
  const canSave =
    !!projectId &&
    mode !== null &&
    (mode !== 'threshold' || (Number.isInteger(thresholdNum) && thresholdNum > 0))

  const save = useMutation({
    mutationFn: () =>
      setMergePolicy(projectId, {
        mode: mode as MergePolicyMode,
        diff_line_threshold: mode === 'threshold' ? thresholdNum : 0,
      }),
    onSuccess: (p) => {
      toast.success(`已保存「${MODE_LABELS[p.mode] ?? p.mode}」合并策略`)
      qc.invalidateQueries({ queryKey: ['merge-policy', projectId] })
      setOpen(false)
      setProjectId('')
      setMode(null)
      setThreshold('0')
    },
    onError: (e: Error) => toast.error(e.message),
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant='ghost' size='lg' aria-label='合并策略'>
          <SlidersHorizontal /> 合并策略
        </Button>
      </DialogTrigger>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>项目合并策略</DialogTitle>
          <DialogDescription>
            策略按项目生效：决定 verdict PASS 后自动合并还是交由人工确认
          </DialogDescription>
        </DialogHeader>
        <div className='grid gap-4'>
          <div className='grid gap-2'>
            <Label>项目</Label>
            <Select
              value={projectId}
              onValueChange={(v) => {
                setProjectId(v)
                setMode(null) // 换项目后跟随其当前策略
              }}
            >
              <SelectTrigger aria-label='选择项目'>
                <SelectValue placeholder='选择项目…' />
              </SelectTrigger>
              <SelectContent>
                {(projects ?? []).map((p) => (
                  <SelectItem key={p.id} value={p.id}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='grid gap-2'>
            <Label>档位</Label>
            <div role='radiogroup' aria-label='合并档位' className='grid gap-2'>
              {MODE_OPTIONS.map((o) => (
                <button
                  key={o.value}
                  type='button'
                  role='radio'
                  aria-checked={mode === o.value}
                  disabled={!projectId}
                  onClick={() => setMode(o.value)}
                  className={cn(
                    'flex items-start gap-3 rounded-lg border p-3 text-left transition-colors disabled:opacity-50',
                    mode === o.value && 'border-foreground bg-muted/50'
                  )}
                >
                  <span
                    aria-hidden
                    className={cn(
                      'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border',
                      mode === o.value && 'border-foreground'
                    )}
                  >
                    {mode === o.value && (
                      <span className='size-2 rounded-full bg-foreground' />
                    )}
                  </span>
                  <span>
                    <span className='block text-sm font-medium'>{o.label}</span>
                    <span className='block text-xs text-muted-foreground'>
                      {o.hint}
                    </span>
                  </span>
                </button>
              ))}
            </div>
          </div>
          {mode === 'threshold' && (
            <div className='grid gap-2'>
              <Label htmlFor='merge-threshold'>diff 行数阈值</Label>
              <Input
                id='merge-threshold'
                aria-label='diff 行数阈值'
                type='number'
                min={1}
                step={1}
                className='w-32 tabular-nums'
                value={threshold}
                onChange={(e) => setThreshold(e.target.value)}
              />
              <p className='text-xs text-muted-foreground'>
                低于该行数自动合并，达到则弹出合并卡待人工确认
              </p>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button
            disabled={!canSave || save.isPending}
            onClick={() => save.mutate()}
          >
            保存策略
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
