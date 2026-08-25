/**
 * Agent 执行时序折线图 option 装配（纯函数，可单测）：
 * 每个 agent 一条曲线、X 轴本地时间轴、Y 轴执行次数（整数刻度）、
 * legend 显隐切换、axis tooltip（时间点 + agent + 次数）。
 * 类型只做 type-only 按需组合，不引入运行时全量 echarts。
 */
import type { ComposeOption } from 'echarts/core'
import type { LineSeriesOption } from 'echarts/charts'
import type {
  GridComponentOption,
  LegendComponentOption,
  TooltipComponentOption,
} from 'echarts/components'
import type { AgentActivityResponse } from './types'

export type ActivityChartOption = ComposeOption<
  | LineSeriesOption
  | GridComponentOption
  | LegendComponentOption
  | TooltipComponentOption
>

/** tooltip formatter 收到的行（axis 触发为数组；只取用到的字段） */
interface TooltipParam {
  name?: unknown
  seriesName?: unknown
  value?: unknown
  marker?: unknown
}

/**
 * 时序横轴刻度：跨度 ≤ 24h 只显 HH:mm，跨天显 MM-DD HH:mm（本地时区，
 * 与 projects/dashboard 的 axisTime 同口径；窗口切换 24/12/6h 均落前者）。
 */
function bucketLabel(iso: string, spanMs: number): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  if (spanMs <= 24 * 3600_000) return hm
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hm}`
}

function tooltipFormatter(params: unknown): string {
  const list = (Array.isArray(params) ? params : [params]) as TooltipParam[]
  if (!list.length) return ''
  const line = (p: TooltipParam) => {
    const marker = typeof p.marker === 'string' ? p.marker : ''
    const name = typeof p.seriesName === 'string' ? p.seriesName : ''
    const value = typeof p.value === 'number' ? p.value : Number(p.value ?? 0)
    return `${marker}${name}：${value} 次`
  }
  const head = typeof list[0].name === 'string' ? list[0].name : ''
  return [head, ...list.map(line)].join('<br/>')
}

/**
 * 装配折线图 option。palette 为单色系 chart token（运行时从 --chart-1..5
 * 解析，暗/亮主题各自适配）；labelColor 用于轴与 legend 文字。
 */
export function buildAgentActivityOption(
  data: AgentActivityResponse,
  palette: string[],
  style: { labelColor?: string } = {}
): ActivityChartOption {
  const spanMs =
    (new Date(data.window.to).getTime() || 0) -
    (new Date(data.window.from).getTime() || 0)
  // 契约保证各曲线 points 等长对齐（含零桶），取首条铺公共时间轴
  const buckets = data.series[0]?.points ?? []
  const labelColor = style.labelColor

  return {
    color: palette,
    grid: { left: 8, right: 16, top: 36, bottom: 8, containLabel: true },
    legend: {
      top: 0,
      left: 'center',
      // 显式按 series 顺序给出（echarts 也会自动推导；显式化钉住切换载体）
      data: data.series.map((s) => s.agent_name),
      icon: 'roundRect',
      itemWidth: 14,
      itemHeight: 4,
      itemGap: 16,
      textStyle: labelColor ? { color: labelColor } : undefined,
    },
    tooltip: {
      trigger: 'axis',
      confine: true,
      formatter: tooltipFormatter,
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: buckets.map((p) => bucketLabel(p.t, spanMs)),
      axisLabel: labelColor ? { color: labelColor } : undefined,
      axisLine: labelColor ? { lineStyle: { color: labelColor } } : undefined,
    },
    yAxis: {
      type: 'value',
      name: '执行次数',
      minInterval: 1,
      nameTextStyle: labelColor ? { color: labelColor } : undefined,
      axisLabel: labelColor ? { color: labelColor } : undefined,
      splitLine: { lineStyle: { opacity: 0.35 } },
    },
    series: data.series.map((s) => ({
      name: s.agent_name,
      type: 'line' as const,
      symbol: 'circle',
      symbolSize: 4,
      lineStyle: { width: 2 },
      data: s.points.map((p) => p.count),
    })),
  }
}
