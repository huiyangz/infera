/**
 * ECharts 按需注册（INFERA-254）：只引入折线图所需的最小集合，
 * 不引 `echarts` 全量包、不用 echarts-forreact 之类 wrapper。
 * 渲染器选 SVG——线图矢量清晰，且 legend/tooltip 交互走真实 DOM。
 */
// echarts 的注册函数恰好叫 use，改名以避开 react-hooks 的 Hook 名匹配
import { use as registerEChartsModules } from 'echarts/core'
import { LineChart } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from 'echarts/components'
import { LegacyGridContainLabel } from 'echarts/features'
import { SVGRenderer } from 'echarts/renderers'

registerEChartsModules([
  LineChart,
  GridComponent,
  LegendComponent,
  TooltipComponent,
  // v6 起 grid.containLabel 为 legacy 特性，需显式注册（否则警告并退化为不含轴标签）
  LegacyGridContainLabel,
  SVGRenderer,
])
