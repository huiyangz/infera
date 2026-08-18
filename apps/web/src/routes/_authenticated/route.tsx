import { createFileRoute, redirect } from '@tanstack/react-router'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'
import { me } from '@/lib/infera-api'

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async () => {
    // me() 仅在未登录时返回 false；网络异常/5xx 会如实抛错，
    // 交给路由错误边界展示，不再吞成「未登录」误导跳登录页。
    const { logged_in } = await me()
    if (!logged_in) {
      throw redirect({ to: '/sign-in' })
    }
  },
  component: AuthenticatedLayout,
})
