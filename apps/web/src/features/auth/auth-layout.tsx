import { Workflow } from 'lucide-react'

type AuthLayoutProps = {
  children: React.ReactNode
}

const FLOW = ['intake', 'spec', 'tests', 'code', 'review']

/** 登录布局（DESIGN.md 编辑风）：左墨黑舞台宣言，右纸白表单；移动端仅表单。 */
export function AuthLayout({ children }: AuthLayoutProps) {
  return (
    <div className='grid h-svh md:grid-cols-2'>
      <aside className='relative hidden flex-col justify-between overflow-hidden bg-[#030303] p-10 text-white md:flex xl:p-14'>
        <div className='flex items-center gap-2.5'>
          <span className='flex size-8 items-center justify-center rounded-lg bg-white text-[#030303]'>
            <Workflow className='size-4' strokeWidth={2} />
          </span>
          <span className='text-lg font-semibold tracking-[-0.4px] lowercase'>
            infera
          </span>
        </div>

        <div className='max-w-md'>
          <p
            className='text-sm font-medium uppercase text-[#939393]'
            style={{ letterSpacing: '0.35px' }}
          >
            Agent 交付流水线
          </p>
          <h2
            className='mt-4 text-5xl font-normal leading-[1.05] xl:text-6xl'
            style={{ letterSpacing: '-1.2px' }}
          >
            你提需求，
            <br />
            Agent 交付。
          </h2>
          <p className='mt-6 max-w-sm text-base leading-relaxed text-[#676f7b]'>
            从一句话需求到合并请求：规格、测试、实现、审查由
            agent 流水线全程推进，人只做关键决策。
          </p>
        </div>

        <div className='border-t border-[#1a1a1a] pt-5'>
          <ol className='flex flex-wrap items-center gap-x-3 gap-y-2'>
            {FLOW.map((s, i) => (
              <li key={s} className='flex items-center gap-3'>
                <span
                  className='text-[11px] font-medium uppercase text-[#939393]'
                  style={{ letterSpacing: '0.2px' }}
                >
                  {s}
                </span>
                {i < FLOW.length - 1 && (
                  <span aria-hidden className='h-px w-6 bg-[#404040]' />
                )}
              </li>
            ))}
          </ol>
        </div>
      </aside>

      <main className='flex flex-col items-center justify-center bg-background px-6 py-10'>
        <div className='mb-6 flex items-center gap-2.5 md:hidden'>
          <span className='flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground'>
            <Workflow className='size-4' strokeWidth={2} />
          </span>
          <span className='text-lg font-semibold tracking-[-0.4px] lowercase'>
            infera
          </span>
        </div>
        <div className='w-full max-w-sm'>{children}</div>
      </main>
    </div>
  )
}
