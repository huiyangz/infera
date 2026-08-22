import { createFileRoute } from '@tanstack/react-router'
import { DecisionsPage } from '@/features/decisions/decisions-page'

export const Route = createFileRoute('/_authenticated/decisions')({
  component: DecisionsPage,
})
