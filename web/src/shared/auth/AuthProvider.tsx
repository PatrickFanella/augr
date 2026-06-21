/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { getCurrentUser, login as loginRequest } from '@/shared/api/endpoints'
import { refreshAccessToken, setRefreshFailureHandler } from '@/shared/auth/refresh'
import { clearTokenSnapshot, getRefreshToken, getTokenSnapshot, setTokenSnapshot } from '@/shared/auth/tokenStore'
import type { AuthSession, LoginRequest } from '@/shared/types/auth'

type AuthStatus = 'checking' | 'authenticated' | 'anonymous'

type AuthContextValue = {
  status: AuthStatus
  session: AuthSession | null
  login: (request: LoginRequest) => Promise<void>
  logout: () => void
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<AuthStatus>('checking')
  const [session, setSession] = useState<AuthSession | null>(null)

  const cleanup = useCallback(() => {
    clearTokenSnapshot()
    setSession(null)
    setStatus('anonymous')
    queryClient.cancelQueries()
    queryClient.clear()
  }, [queryClient])

  useEffect(() => {
    setRefreshFailureHandler(cleanup)
    return () => setRefreshFailureHandler(null)
  }, [cleanup])

  useEffect(() => {
    let active = true
    async function bootstrap() {
      try {
        let tokens = getTokenSnapshot()
        if (!tokens?.access_token && getRefreshToken()) {
          await refreshAccessToken()
          tokens = getTokenSnapshot()
        }
        if (!tokens?.access_token) {
          if (active) setStatus('anonymous')
          return
        }
        const user = await getCurrentUser()
        if (!active) return
        setSession({ user, ...tokens })
        setStatus('authenticated')
      } catch {
        if (active) cleanup()
      }
    }
    void bootstrap()
    return () => {
      active = false
    }
  }, [cleanup])

  const login = useCallback(async (request: LoginRequest) => {
    const tokens = await loginRequest(request)
    setTokenSnapshot(tokens)
    try {
      const user = await getCurrentUser()
      setSession({ user, ...tokens })
      setStatus('authenticated')
    } catch (error) {
      clearTokenSnapshot()
      throw error
    }
  }, [])

  const value = useMemo(() => ({ status, session, login, logout: cleanup }), [cleanup, login, session, status])
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used within AuthProvider')
  return value
}
