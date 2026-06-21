/* eslint-disable react-refresh/only-export-components */
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { appConfig } from '@/app/config/env'
import { parseContract } from '@/shared/api/contract'
import { websocketEventEnvelopeSchema } from '@/shared/api/schemas'
import { refreshAccessToken } from '@/shared/auth/refresh'
import { getAccessToken, isAccessTokenExpiringSoon } from '@/shared/auth/tokenStore'
import { redactUrl } from '@/shared/logging/redact'
import type { WebSocketClientCommand, WebSocketEventEnvelope } from '@/shared/types/websocket'

type RealtimeStatus = 'idle' | 'connecting' | 'connected' | 'disconnected' | 'degraded'

type RealtimeContextValue = {
  status: RealtimeStatus
  events: WebSocketEventEnvelope[]
  failedAttempts: number
  send: (command: WebSocketClientCommand) => void
  disconnect: () => void
}

const RealtimeContext = createContext<RealtimeContextValue | null>(null)
const maxEvents = 250

export function RealtimeProvider({ authenticated, children }: { authenticated: boolean; children: ReactNode }) {
  const socketRef = useRef<WebSocket | null>(null)
  const subscriptionsRef = useRef<WebSocketClientCommand[]>([])
  const reconnectTimerRef = useRef<number | null>(null)
  const shouldReconnectRef = useRef(false)
  const failedAttemptsRef = useRef(0)
  const connectionEpochRef = useRef(0)
  const [status, setStatus] = useState<RealtimeStatus>('idle')
  const [events, setEvents] = useState<WebSocketEventEnvelope[]>([])
  const [failedAttempts, setFailedAttempts] = useState(0)

  const clearTimer = useCallback(() => {
    if (reconnectTimerRef.current !== null) window.clearTimeout(reconnectTimerRef.current)
    reconnectTimerRef.current = null
  }, [])

  const disconnect = useCallback(() => {
    connectionEpochRef.current += 1
    shouldReconnectRef.current = false
    clearTimer()
    const socket = socketRef.current
    if (socket) {
      socket.onopen = null
      socket.onmessage = null
      socket.onerror = null
      socket.onclose = null
      socket.close()
    }
    socketRef.current = null
    subscriptionsRef.current = []
    failedAttemptsRef.current = 0
    setFailedAttempts(0)
    setEvents([])
    setStatus('idle')
  }, [clearTimer])

  const send = useCallback((command: WebSocketClientCommand) => {
    const commandKey = JSON.stringify(command)
    if (!subscriptionsRef.current.some((item) => JSON.stringify(item) === commandKey)) {
      subscriptionsRef.current = [...subscriptionsRef.current, command]
    }
    const payload = JSON.stringify(command)
    const socket = socketRef.current
    const openState = typeof WebSocket.OPEN === 'number' ? WebSocket.OPEN : 1
    if (socket !== null && socket.readyState === openState) socket.send(payload)
  }, [])

  const connect = useCallback(async () => {
    if (!authenticated) return
    const connectionEpoch = connectionEpochRef.current + 1
    connectionEpochRef.current = connectionEpoch
    shouldReconnectRef.current = true
    setStatus('connecting')
    const previousSocket = socketRef.current
    if (previousSocket) {
      previousSocket.onopen = null
      previousSocket.onmessage = null
      previousSocket.onerror = null
      previousSocket.onclose = null
      previousSocket.close()
      socketRef.current = null
    }
    if (isAccessTokenExpiringSoon()) await refreshAccessToken().catch(() => undefined)
    if (!shouldReconnectRef.current || connectionEpochRef.current !== connectionEpoch) return
    const token = getAccessToken()
    if (!token) return
    const url = new URL(appConfig.wsBaseUrl, window.location.origin)
    url.searchParams.set('token', token)
    const socket = new WebSocket(url.toString())
    socketRef.current = socket

    socket.onopen = () => {
      if (socketRef.current !== socket || connectionEpochRef.current !== connectionEpoch) return
      failedAttemptsRef.current = 0
      setFailedAttempts(0)
      setStatus('connected')
      for (const command of subscriptionsRef.current) socket.send(JSON.stringify(command))
    }
    socket.onmessage = (message) => {
      if (socketRef.current !== socket || connectionEpochRef.current !== connectionEpoch) return
      try {
        const event = parseContract('WebSocket event', websocketEventEnvelopeSchema, JSON.parse(String(message.data)))
        setEvents((current) => [event, ...current].slice(0, maxEvents))
      } catch {
        // Ignore malformed realtime messages; REST remains canonical.
      }
    }
    socket.onerror = () => {
      if (socketRef.current !== socket || connectionEpochRef.current !== connectionEpoch) return
      // Do not log token-bearing URL.
      redactUrl(url.toString())
    }
    socket.onclose = () => {
      if (socketRef.current !== socket || connectionEpochRef.current !== connectionEpoch) return
      socketRef.current = null
      if (!shouldReconnectRef.current) return
      failedAttemptsRef.current += 1
      setFailedAttempts(failedAttemptsRef.current)
      setStatus(failedAttemptsRef.current >= 5 ? 'degraded' : 'disconnected')
      const delayMs = Math.min(30_000, 1000 * 2 ** Math.min(failedAttemptsRef.current - 1, 3))
      reconnectTimerRef.current = window.setTimeout(() => void connect(), delayMs)
    }
  }, [authenticated])

  useEffect(() => {
    if (authenticated) void connect()
    else disconnect()
    return disconnect
  }, [authenticated, connect, disconnect])

  const value = useMemo(() => ({ status, events, failedAttempts, send, disconnect }), [disconnect, events, failedAttempts, send, status])
  return <RealtimeContext.Provider value={value}>{children}</RealtimeContext.Provider>
}

export function useRealtime() {
  const value = useContext(RealtimeContext)
  if (!value) throw new Error('useRealtime must be used within RealtimeProvider')
  return value
}
