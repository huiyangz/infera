import { createFileRoute } from '@tanstack/react-router'
import { GatePage } from '@/features/deliveries/gate'

export const Route = createFileRoute('/_authenticated/deliveries/$id/gate')({
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  return <GatePage deliveryId={id} />
}
