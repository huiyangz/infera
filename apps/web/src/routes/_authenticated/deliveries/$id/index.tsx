import { createFileRoute } from '@tanstack/react-router'
import { DeliveryDetail } from '@/features/deliveries/delivery-detail'

// 需求详情页：项目任务列表 / 深链整页进入（原「重定向到项目页主从布局」
// 已随项目域重构移除，改为独立页面渲染）。/gate 子路由不受影响。
export const Route = createFileRoute('/_authenticated/deliveries/$id/')({
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  return <DeliveryDetail deliveryId={id} />
}
