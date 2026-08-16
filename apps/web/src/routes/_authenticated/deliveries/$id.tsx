import { createFileRoute } from '@tanstack/react-router'
import { DeliveryDetail } from '@/features/deliveries/delivery-detail'

export const Route = createFileRoute('/_authenticated/deliveries/$id')({
  component: RouteComponent,
})

function RouteComponent() {
  const { id } = Route.useParams()
  return <DeliveryDetail deliveryId={id} />
}
