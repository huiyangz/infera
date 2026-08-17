import { Workflow } from 'lucide-react'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  return (
    <div className='container grid h-svh max-w-none items-center justify-center'>
      <div className='mx-auto flex w-full flex-col justify-center space-y-2 py-8 sm:p-8'>
        <div className='mb-4 flex items-center justify-center gap-2.5'>
          <span className='flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground'>
            <Workflow className='size-4.5' strokeWidth={2} />
          </span>
          <h1 className='text-xl font-semibold tracking-[-0.4px] lowercase'>
            infera
          </h1>
        </div>
        {children}
      </div>
    </div>
  )
}
