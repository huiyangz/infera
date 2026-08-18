import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, type RenderResult } from 'vitest-browser-react'
import { type Locator, userEvent } from 'vitest/browser'
import { UserAuthForm } from './user-auth-form'
import { ApiError, login } from '@/lib/infera-api'

const navigate = vi.fn()
const toastError = vi.fn()
const toastSuccess = vi.fn()

vi.mock('@/lib/infera-api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/infera-api')>()
  return { ...actual, login: vi.fn() }
})

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => navigate,
  }
})

vi.mock('sonner', () => ({
  toast: { error: (...args: unknown[]) => toastError(...args), success: (...args: unknown[]) => toastSuccess(...args) },
}))

describe('UserAuthForm', () => {
  let screen: RenderResult
  let passwordInput: Locator
  let signInButton: Locator

  beforeEach(async () => {
    vi.clearAllMocks()
    vi.mocked(login).mockReset()
    screen = await render(<UserAuthForm />)
    passwordInput = screen.getByLabelText(/^访问密码$/i)
    signInButton = screen.getByRole('button', { name: /^登录$/ })
  })

  it('renders the password field and submit button', async () => {
    await expect.element(passwordInput).toBeInTheDocument()
    await expect.element(signInButton).toBeInTheDocument()
  })

  it('shows a validation message when submitting an empty password', async () => {
    await userEvent.click(signInButton)

    await expect.element(screen.getByText('请输入密码')).toBeInTheDocument()
    expect(login).not.toHaveBeenCalled()
  })

  it('navigates to the default route on success', async () => {
    vi.mocked(login).mockResolvedValue(undefined)

    await userEvent.fill(passwordInput, 'secret')
    await userEvent.click(signInButton)

    await vi.waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({ to: '/', replace: true })
    )
    expect(toastError).not.toHaveBeenCalled()
  })

  it('navigates to redirectTo when provided', async () => {
    vi.mocked(login).mockResolvedValue(undefined)

    // beforeEach 已渲染一个无 redirectTo 的表单且未卸载：用 .last() 锁定本次渲染的表单
    const { getByLabelText, getByRole } = await render(
      <UserAuthForm redirectTo='/projects/abc' />
    )
    await userEvent.fill(getByLabelText(/访问密码/).last(), 'secret')
    await userEvent.click(getByRole('button', { name: /登录/ }).last())

    await vi.waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: '/projects/abc',
        replace: true,
      })
    )
  })

  it('reports wrong password on 401 and does not navigate', async () => {
    vi.mocked(login).mockRejectedValue(new ApiError(401, '密码错误'))

    await userEvent.fill(passwordInput, 'wrong')
    await userEvent.click(signInButton)

    await vi.waitFor(() => expect(toastError).toHaveBeenCalledWith('密码错误'))
    expect(navigate).not.toHaveBeenCalled()
  })

  it('reports connection failure on network/server errors instead of blaming the password', async () => {
    vi.mocked(login).mockRejectedValue(new TypeError('Failed to fetch'))

    await userEvent.fill(passwordInput, 'secret')
    await userEvent.click(signInButton)

    await vi.waitFor(() =>
      expect(toastError).toHaveBeenCalledWith('无法连接服务器，请稍后重试')
    )
    expect(toastError).not.toHaveBeenCalledWith('密码错误')
    expect(navigate).not.toHaveBeenCalled()
  })

  it('recovers the button state after a failed attempt', async () => {
    vi.mocked(login).mockRejectedValue(new ApiError(401, '密码错误'))

    await userEvent.fill(passwordInput, 'wrong')
    await userEvent.click(signInButton)

    await vi.waitFor(() => expect(toastError).toHaveBeenCalled())
    await expect.element(signInButton).toBeEnabled()
  })
})
