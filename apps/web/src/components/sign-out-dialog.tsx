import { useNavigate, useLocation } from '@tanstack/react-router'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { logout } from '@/lib/infera-api'

interface SignOutDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function SignOutDialog({ open, onOpenChange }: SignOutDialogProps) {
  const navigate = useNavigate()
  const location = useLocation()

  const handleSignOut = () => {
    const currentPath = location.href
    // ConfirmDialog 不 await handleConfirm，这里 fire-and-forget：
    // 先请求后端撤销 session（HttpOnly cookie 只能由后端清），无论成败都回登录页。
    logout()
      .catch(() => false)
      .finally(() => {
        // Preserve current location for redirect after sign-in
        navigate({
          to: '/sign-in',
          search: { redirect: currentPath },
          replace: true,
        })
      })
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title='Sign out'
      desc='Are you sure you want to sign out? You will need to sign in again to access your account.'
      confirmText='Sign out'
      destructive
      handleConfirm={handleSignOut}
      className='sm:max-w-sm'
    />
  )
}
