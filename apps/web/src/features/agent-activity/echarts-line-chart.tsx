/**
 * 受控 ECharts React 封装：挂载 init（SVG 渲染器）→ setOption(option)、
 * option 变更增量 setOption（不重建实例）、容器尺寸变化 resize
 * （ResizeObserver）、卸载 dispose。不做数据装配——option 由调用方
 * （buildAgentActivityOption）以受控 prop 传入。
 */
import { useEffect, useRef } from 'react'
import { init } from 'echarts/core'
import { cn } from '@/lib/utils'
import './echarts-setup'
import type { ActivityChartOption } from './chart-option'

interface ActivityChartProps {
  option: ActivityChartOption
  /** 无障碍名（图表以 canvas/svg 呈现，文字不可达） */
  'aria-label'?: string
  className?: string
}

export function ActivityChart({
  option,
  'aria-label': ariaLabel,
  className,
}: ActivityChartProps) {
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
