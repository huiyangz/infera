/**
 * 执行时段分布直方图 option 装配（纯函数，可单测）：
 * X 轴 24 个本地小时桶（00时..23时）、双 Y 轴（左执行次数、右累计分钟）、
 * 夜间桶（22:00–06:00）柱体全不透明、白天柱降透明度——「晚上跑了多长
 * 时间」一眼可辨。类型只做 type-only 按需组合，不引入运行时全量 echarts
 * （与 agent-activity/chart-option 同款做法，序列换 bar）。
 */
import type { ComposeOption } from 'echarts/core'
import type { BarSeriesOption } from 'echarts/charts'
import type {
  GridComponentOption,
  LegendComponentOption,
  TooltipComponentOption,
} from 'echarts/components'
import { formatDuration } from './format'
import type { WorkspaceStatsResponse } from './types'

export type StatsChartOption = ComposeOption<
  | BarSeriesOption
  | GridComponentOption
  | LegendComponentOption
  | TooltipComponentOption
>

/** 夜间时段边界（约 22:00–06:00，前闭后开）：22/23/0..5 时为夜间 */
export const NIGHT_START_HOUR = 22
export const NIGHT_END_HOUR = 6

export function isNightHour(hour: number): boolean {
  return hour >= NIGHT_START_HOUR || hour < NIGHT_END_HOUR
}

/** 白天柱降透明度（夜间保持全不透明 → 夜间高亮） */
const DAY_BAR_OPACITY = 0.45

interface BarPoint {
  value: number
  itemStyle?: { opacity: number }
}

interface HourStyle {
  palette?: string[]
  labelColor?: string
}

/** tooltip formatter 收到的行（axis 触发为数组；只取用到的字段） */
interface TooltipParam {
  name?: unknown
  seriesName?: unknown
  value?: unknown
  marker?: unknown
  dataIndex?: unknown
}

/** tooltip 时长行：右轴 value 为分钟，换回 ms 走人话格式与数字卡一致 */
function tooltipFormatter(params: unknown): string {
  const list = (Array.isArray(params) ? params : [params]) as TooltipParam[]
  if (!list.length) return ''
  const first = list[0]
  const name = typeof first.name === 'string' ? first.name : ''
  const hour = typeof first.dataIndex === 'number' ? first.dataIndex : -1
  const head = `${name}${hour >= 0 && isNightHour(hour) ? '（夜间）' : ''}`
  const line = (p: TooltipParam) => {
    const marker = typeof p.marker === 'string' ? p.marker : ''
    const seriesName = typeof p.seriesName === 'string' ? p.seriesName : ''
    const value = typeof p.value === 'number' ? p.value : Number(p.value ?? 0)
    if (seriesName === '累计时长') {
      return `${marker}${seriesName}：${formatDuration(value * 60_000)}`
    }
    return `${marker}${seriesName}：${value} 次`
  }
  return [head, ...list.map(line)].join('<br/>')
}

/** 单序列柱点：白天柱降透明度，夜间柱不带覆盖（全不透明高亮） */
function barPoints(
  buckets: WorkspaceStatsResponse['hourly'],
  pick: (b: WorkspaceStatsResponse['hourly'][number]) => number
): BarPoint[] {
  return buckets.map((b) => {
    const point: BarPoint = { value: pick(b) }
    if (!isNightHour(b.hour)) {
      point.itemStyle = { opacity: DAY_BAR_OPACITY }
    }
    return point
  })
}

/**
 * 装配时段分布直方图 option。palette 为单色系 chart token（运行时从
 * --chart-1..5 解析，暗/亮主题各自适配）；labelColor 用于轴与 legend 文字。
 */
export function buildHourlyOption(
  data: WorkspaceStatsResponse,
  style: HourStyle = {}
): StatsChartOption {
  const palette = style.palette ?? ['#030303', '#404040', '#676f7b', '#939393', '#e7eaf0']
  const labelColor = style.labelColor
  const label = labelColor ? { color: labelColor } : undefined
  // 契约保证 0..23 升序补零；排序为防御性兜底（不改变口径）
  const buckets = [...(data.hourly ?? [])].sort((a, b) => a.hour - b.hour)

  return {
    color: palette,
    grid: { left: 8, right: 8, top: 36, bottom: 8, containLabel: true },
    legend: {
      top: 0,
      left: 'center',
      data: ['执行次数', '累计时长'],
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
      data: buckets.map((b) => `${String(b.hour).padStart(2, '0')}时`),
      axisLabel: label,
      axisLine: labelColor ? { lineStyle: { color: labelColor } } : undefined,
    },
    yAxis: [
      {
        type: 'value',
        name: '执行次数',
        minInterval: 1,
        nameTextStyle: label,
        axisLabel: label,
        splitLine: { lineStyle: { opacity: 0.35 } },
      },
      {
        type: 'value',
        name: '累计时长（分钟）',
        position: 'right',
        minInterval: 1,
        nameTextStyle: label,
        axisLabel: label,
        splitLine: { show: false },
      },
    ],
    series: [
      {
        name: '执行次数',
        type: 'bar' as const,
        yAxisIndex: 0,
        barMaxWidth: 18,
        itemStyle: { color: palette[0] },
        data: barPoints(buckets, (b) => b.runs),
      },
      {
        name: '累计时长',
        type: 'bar' as const,
        yAxisIndex: 1,
        barMaxWidth: 18,
        itemStyle: { color: palette[3] ?? palette[1] },
        data: barPoints(buckets, (b) => Math.round(b.duration_ms / 60_000)),
      },
    ],
  }
}
