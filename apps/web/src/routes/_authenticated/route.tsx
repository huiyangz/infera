import { createFileRoute, redirect } from '@tanstack/react-router'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'
import { me } from '@/lib/infera-api'

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async () => {
    const { logged_in } = await me().catch(() => ({ logged_in: false }))
    if (!logged_in) {
      throw redirect({ to: '/sign-in' })
    }
  },
  component: AuthenticatedLayout,
})
