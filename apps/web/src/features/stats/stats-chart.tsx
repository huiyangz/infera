/**
 * 受控 ECharts React 封装（柱状直方图）：挂载 init（SVG 渲染器）→
 * setOption(option)、option 变更增量 setOption（不重建实例）、容器尺寸
 * 变化 resize（ResizeObserver）、卸载 dispose。不做数据装配——option 由
 * 调用方（buildHourlyOption）以受控 prop 传入。生命周期与
 * agent-activity/echarts-line-chart 同构（该封装的 option 类型与注册
 * 集合均限定折线图，本页不复用以免改动 INFERA-252 交付物）。
 */
import { useEffect, useRef } from 'react'
import { init } from 'echarts/core'
import { cn } from '@/lib/utils'
import './echarts-setup'
import type { StatsChartOption } from './chart-option'

interface StatsChartProps {
  option: StatsChartOption
  /** 无障碍名（图表以 svg 呈现，文字不可达） */
  'aria-label'?: string
  className?: string
}

export function StatsChart({
  option,
  'aria-label': ariaLabel,
  className,
}: StatsChartProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<ReturnType<typeof init> | null>(null)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const chart = init(host, undefined, { renderer: 'svg' })
    chartRef.current = chart
    const ro = new ResizeObserver(() => chart.resize())
    ro.observe(host)
    return () => {
      ro.disconnect()
      chart.dispose()
      chartRef.current = null
    }
  }, [])

  useEffect(() => {
    chartRef.current?.setOption(option)
  }, [option])

  return (
    <div
      ref={hostRef}
      role='img'
      aria-label={ariaLabel}
      className={cn('h-80 w-full', className)}
    />
  )
}
