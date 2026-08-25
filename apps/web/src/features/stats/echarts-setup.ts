/**
 * ECharts 按需注册（统计页直方图）：只引入柱状图所需的最小集合，
 * 不引 `echarts` 全量包。渲染器选 SVG——矢量清晰，且 legend/tooltip
 * 交互走真实 DOM（与 agent-activity/echarts-setup 同款做法；本页只需
 * Bar，不复用该文件以免改动 INFERA-252 交付物——其注册集合为折线图）。
 */
// echarts 的注册函数恰好叫 use，改名以避开 react-hooks 的 Hook 名匹配
import { use as registerEChartsModules } from 'echarts/core'
import { BarChart } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from 'echarts/components'
import { LegacyGridContainLabel } from 'echarts/features'
import { SVGRenderer } from 'echarts/renderers'

registerEChartsModules([
  BarChart,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  // v6 起 grid.containLabel 为 legacy 特性，需显式注册（否则警告并退化为不含轴标签）
  LegacyGridContainLabel,
  SVGRenderer,
])
