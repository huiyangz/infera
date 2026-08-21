import { createFileRoute } from '@tanstack/react-router'
import { RequirementsList } from '@/features/requirements/requirements-list'

export const Route = createFileRoute('/_authenticated/requirements/')({
  component: RouteComponent,
})

function RouteComponent() {
  return <RequirementsList />
}
