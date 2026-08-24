import { createFileRoute } from '@tanstack/react-router'
import { DiscoveryPage } from '@/features/discovery/discovery-page'

export const Route = createFileRoute('/_authenticated/discovery')({
  component: DiscoveryPage,
})
