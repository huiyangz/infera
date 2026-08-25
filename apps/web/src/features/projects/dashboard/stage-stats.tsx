import {
  type StageRunStageStats,
  stageLabel,
  stagesForDelivery,
} from '@/lib/infera-types'
import { cn } from '@/lib/utils'
import { formatDuration } from './format'

/**
 * 流水线全 11 阶段全序（经 stagesForDelivery 大复杂度口径派生，
 * 不自行重定义——T01 契约注释点名消费方须按阶段序重排 by_stage）。
 * 放宽为 string 序列以容纳 indexOf(未知阶段) 的 -1 殿后判断。
 */
const STAGE_ORDER: readonly string[] = stagesForDelivery({
  complexity: 'large',
})

/**
 * 分 stage 聚合健康度表（INFERA-243）：by_stage 后端按 stage 字典序返回，
 * 这里按流水线阶段序重排、未知阶段殿后；失败计数 >0 加重。
 * avg_ms/p95_ms 为 0 = 契约里「无已收尾运行」，显示「—」而非「0 秒」。
 */
export function StageStatsTable({ byStage }: { byStage: StageRunStageStats[] }) {
  if (byStage.length === 0) return null

  const rows = [...byStage].sort((a, b) => {
    const ia = STAGE_ORDER.indexOf(a.stage)
    const ib = STAGE_ORDER.indexOf(b.stage)
    return (
      (ia === -1 ? STAGE_ORDER.length : ia) -
      (ib === -1 ? STAGE_ORDER.length : ib)
    )
  })

  return (
    <div className='overflow-x-auto'>
      <table data-stage-stats className='w-full text-sm'>
        <thead>
          <tr className='text-left text-[11px] tracking-wider text-muted-foreground uppercase'>
            <th className='py-2 pr-4 font-medium'>阶段</th>
            <th className='py-2 pr-4 text-right font-medium'>运行</th>
            <th className='py-2 pr-4 text-right font-medium'>失败</th>
            <th className='py-2 pr-4 text-right font-medium'>进行中</th>
            <th className='py-2 pr-4 text-right font-medium'>平均耗时</th>
            <th className='py-2 text-right font-medium'>P95</th>
          </tr>
        </thead>
        <tbody className='divide-y'>
          {rows.map((r) => (
            <tr key={r.stage} data-stage={r.stage}>
              <td className='py-2 pr-4 whitespace-nowrap'>{stageLabel(r.stage)}</td>
              <td className='py-2 pr-4 text-right tabular-nums'>{r.total}</td>
              <td
                className={cn(
                  'py-2 pr-4 text-right tabular-nums',
                  r.failed > 0 && 'font-medium'
                )}
              >
                {r.failed}
              </td>
              <td className='py-2 pr-4 text-right tabular-nums'>{r.running}</td>
              <td className='py-2 pr-4 text-right text-muted-foreground tabular-nums'>
                {r.avg_ms > 0 ? formatDuration(r.avg_ms) : '—'}
              </td>
              <td className='py-2 text-right text-muted-foreground tabular-nums'>
                {r.p95_ms > 0 ? formatDuration(r.p95_ms) : '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
