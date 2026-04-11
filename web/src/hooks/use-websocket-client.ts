import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { getWebSocketUrl } from '@/lib/config'
import { getAccessToken } from '@/lib/auth'
import type { UUID, WebSocketServerMessage, WebSocketSubscriptionCommand } from '@/lib/api/types'

export type WebSocketConnectionStatus = 'idle' | 'connecting' | 'open' | 'closed' | 'error'

interface UseWebSocketClientOptions {
  enabled?: boolean
  reconnect?: boolean
  reconnectDelayMs?: number
  url?: string
  onMessage?: (message: WebSocketServerMessage) => void
  onError?: (event: Event) => void
}

function parseServerMessage(payload: string): WebSocketServerMessage | null {
  try {
    return JSON.parse(payload) as WebSocketServerMessage
  } catch {
    return null
  }
}

export function useWebSocketClient({
  enabled = true,
  reconnect = true,
  reconnectDelayMs = 2_000,
  url,
  onMessage,
  onError,
}: UseWebSocketClientOptions = {}) {
  const socketRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<number | null>(null)
  const shouldReconnectRef = useRef(enabled && reconnect)
  const [status, setStatus] = useState<WebSocketConnectionStatus>('idle')
  const [lastMessage, setLastMessage] = useState<WebSocketServerMessage | null>(null)
  const endpoint = useMemo(() => url ?? getWebSocketUrl(), [url])

  const resolveEndpoint = useCallback(() => {
    const token = getAccessToken()
    if (!token) {
      return endpoint
    }

    try {
      const parsed = new URL(endpoint)
      if (!parsed.searchParams.has('token') && !parsed.searchParams.has('api_key')) {
        parsed.searchParams.set('token', token)
      }
      return parsed.toString()
    } catch {
      return endpoint
    }
  }, [endpoint])

  const clearReconnectTimer = useCallback(() => {
    if (reconnectTimerRef.current !== null) {
      window.clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
  }, [])

  const disconnect = useCallback(() => {
    shouldReconnectRef.current = false
    clearReconnectTimer()
    const socket = socketRef.current
    socketRef.current = null
    if (socket && socket.readyState < WebSocket.CLOSING) {
      socket.close()
    }
    setStatus('closed')
  }, [clearReconnectTimer])

  const connect = useCallback(() => {
    const currentSocket = socketRef.current
    if (currentSocket && currentSocket.readyState <= WebSocket.OPEN) {
      return
    }

    clearReconnectTimer()
    shouldReconnectRef.current = enabled && reconnect
    setStatus('connecting')

    const socket = new WebSocket(resolveEndpoint())
    socketRef.current = socket

    socket.onopen = () => {
      setStatus('open')
    }

    socket.onmessage = (event) => {
      const parsed = parseServerMessage(String(event.data))
      if (!parsed) {
        return
      }
      setLastMessage(parsed)
      onMessage?.(parsed)
    }

    socket.onerror = (event) => {
      setStatus('error')
      onError?.(event)
    }

    socket.onclose = () => {
      const isCurrentSocket = socketRef.current === socket
      const wasReplacedByNewerConnection = !isCurrentSocket && socketRef.current !== null
      const shouldNotReconnect = !shouldReconnectRef.current

      if (isCurrentSocket) {
        socketRef.current = null
      }
      setStatus('closed')
      if (wasReplacedByNewerConnection || shouldNotReconnect) {
        return
      }
      if (shouldReconnectRef.current) {
        reconnectTimerRef.current = window.setTimeout(() => {
          connect()
        }, reconnectDelayMs)
      }
    }
  }, [clearReconnectTimer, enabled, onError, onMessage, reconnect, reconnectDelayMs, resolveEndpoint])

  const sendCommand = useCallback((command: WebSocketSubscriptionCommand | { action: 'subscribe_all' | 'unsubscribe_all' }) => {
    const socket = socketRef.current
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return false
    }

    socket.send(JSON.stringify(command))
    return true
  }, [])

  const subscribe = useCallback(
    (subscription: Omit<WebSocketSubscriptionCommand, 'action'> = {}) =>
      sendCommand({ action: 'subscribe', ...subscription }),
    [sendCommand],
  )

  const unsubscribe = useCallback(
    (subscription: Omit<WebSocketSubscriptionCommand, 'action'> = {}) =>
      sendCommand({ action: 'unsubscribe', ...subscription }),
    [sendCommand],
  )

  const subscribeAll = useCallback(() => sendCommand({ action: 'subscribe_all' }), [sendCommand])
  const unsubscribeAll = useCallback(() => sendCommand({ action: 'unsubscribe_all' }), [sendCommand])

  useEffect(() => {
    shouldReconnectRef.current = enabled && reconnect
    if (!enabled) {
      disconnect()
      return undefined
    }

    connect()
    return () => {
      disconnect()
    }
  }, [connect, disconnect, enabled, reconnect])

  return {
    status,
    lastMessage,
    connect,
    disconnect,
    sendCommand,
    subscribe,
    unsubscribe,
    subscribeAll,
    unsubscribeAll,
  }
}

export function createStrategySubscription(strategyIds: UUID[] = [], runIds: UUID[] = []) {
  return {
    strategy_ids: strategyIds,
    run_ids: runIds,
  }
}
