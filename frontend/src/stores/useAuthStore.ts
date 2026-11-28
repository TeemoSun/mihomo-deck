import { create } from 'zustand'
import { api, ApiError } from '../api/client'
import type { AuthUser } from '../types/api'
import { useLiveStore } from './useLiveStore'
import { useToastStore } from './useToastStore'

interface State {
  user: AuthUser | null
  csrfToken: string
  checking: boolean
  sessionSignal: AbortSignal
  setCsrfToken: (token: string) => void
  clearSession: () => void
  login: (username: string, password: string) => Promise<void>
  checkAuth: () => Promise<void>
  logout: () => Promise<void>
  updatePassword: (currentPassword: string, newPassword: string) => Promise<void>
}

export const useAuthStore = create<State>((set, get) => {
  let authCheck: Promise<void> | undefined
  let sessionController = new AbortController()

  const nextSession = () => {
    sessionController.abort(new DOMException('会话已变更', 'AbortError'))
    sessionController = new AbortController()
    return sessionController.signal
  }

  return {
    user: null,
    csrfToken: '',
    checking: true,
    sessionSignal: sessionController.signal,
    setCsrfToken: csrfToken => set(({ user }) => ({
      user: user ? { ...user, csrfToken } : null,
      csrfToken,
    })),
    clearSession: () => {
      set({ user: null, csrfToken: '', checking: false, sessionSignal: nextSession() })
      useLiveStore.getState().disconnect()
    },
    login: async (username, password) => {
      const sessionSignal = nextSession()
      set({ sessionSignal })
      const user = await api.login(username, password)
      if (!sessionSignal.aborted) set({ user, csrfToken: user.csrfToken, checking: false })
    },
    checkAuth: () => {
      if (authCheck) return authCheck
      if (!get().checking) return Promise.resolve()

      const { sessionSignal } = get()
      authCheck = api.me().then(user => {
        if (!sessionSignal.aborted) set({ user, csrfToken: user.csrfToken })
      }).catch(error => {
        if (!sessionSignal.aborted && error instanceof ApiError && error.status === 401) {
          get().clearSession()
        }
      }).finally(() => {
        if (!sessionSignal.aborted) set({ checking: false })
        authCheck = undefined
      })
      return authCheck
    },
    logout: async () => {
      const { sessionSignal } = get()
      try {
        await api.logout()
      } catch (error) {
        if (sessionSignal.aborted) return
        if (!(error instanceof ApiError && error.status === 401)) {
          useToastStore.getState().error('退出登录失败，请检查网络连接后重试')
          return
        }
      }
      if (!sessionSignal.aborted) get().clearSession()
    },
    updatePassword: async (currentPassword, newPassword) => {
      const { sessionSignal } = get()
      const result = await api.password(currentPassword, newPassword)
      if (!sessionSignal.aborted) get().setCsrfToken(result.csrfToken)
    },
  }
})
