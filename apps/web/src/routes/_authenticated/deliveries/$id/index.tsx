import { createFileRoute, redirect } from '@tanstack/react-router'
import { getDelivery } from '@/lib/infera-api'

// 深链入口：/deliveries/:id 重定向到项目页主从布局（右栏选中该需求）。
// 放在 index 而不是父路由 beforeLoad，否则会把 /gate 子路由一起劫持。
export const Route = createFileRoute('/_authenticated/deliveries/$id/')({
  beforeLoad: async ({ params, context }) => {
    const data = await context.queryClient.ensureQueryData({
      queryKey: ['delivery', params.id],
      queryFn: () => getDelivery(params.id),
    })
    throw redirect({
      to: '/projects/$id',
      params: { id: data.delivery.project_id },
      search: { d: params.id },
    })
  },
})
