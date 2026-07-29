/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { getCurrentUser, login as loginRequest } from '@/shared/api/endpoints'
import { isApiClientError } from '@/shared/api/errors'
import { refreshAccessToken, setRefreshFailureHandler } from '@/shared/auth/refresh'
import { clearTokenSnapshot, getRefreshToken, getTokenSnapshot, setTokenSnapshot } from '@/shared/auth/tokenStore'
import type { AuthSession, LoginRequest } from '@/shared/types/auth'

type AuthStatus = 'checking' | 'authenticated' | 'anonymous' | 'unavailable'

type AuthContextValue = {
  status: AuthStatus
  session: AuthSession | null
  login: (request: LoginRequest) => Promise<void>
  logout: () => void
  retry: () => void
  reason: 'expired' | null
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<AuthStatus>('checking')
  const [session, setSession] = useState<AuthSession | null>(null)
  const [reason, setReason] = useState<'expired' | null>(null)
  const [bootstrapAttempt, setBootstrapAttempt] = useState(0)

  const cleanup = useCallback((nextReason: 'expired' | null = null) => {
    clearTokenSnapshot()
    setSession(null)
    setReason(nextReason)
    setStatus('anonymous')
    queryClient.cancelQueries()
    queryClient.clear()
  }, [queryClient])

  useEffect(() => {
    setRefreshFailureHandler(() => cleanup('expired'))
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
      } catch (error) {
        if (!active) return
        if (isApiClientError(error) && ['unauthorized', 'bad_request', 'validation'].includes(error.kind)) {
          cleanup('expired')
        } else {
          setStatus('unavailable')
        }
      }
    }
    void bootstrap()
    return () => {
      active = false
    }
  }, [bootstrapAttempt, cleanup])

  const login = useCallback(async (request: LoginRequest) => {
    const tokens = await loginRequest(request)
    setTokenSnapshot(tokens)
    try {
      const user = await getCurrentUser()
      setSession({ user, ...tokens })
      setReason(null)
      setStatus('authenticated')
    } catch (error) {
      clearTokenSnapshot()
      throw error
    }
  }, [])

  const logout = useCallback(() => cleanup(null), [cleanup])
  const retry = useCallback(() => {
    setStatus('checking')
    setBootstrapAttempt((attempt) => attempt + 1)
  }, [])
  const value = useMemo(() => ({ status, session, login, logout, retry, reason }), [login, logout, reason, retry, session, status])
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used within AuthProvider')
  return value
}
