import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useWebSocketClient } from '@/hooks/use-websocket-client'

const getAccessTokenMock = vi.hoisted(() => vi.fn())

vi.mock('@/lib/auth', () => ({
  getAccessToken: getAccessTokenMock,
}))

class MockWebSocket {
  static instances: MockWebSocket[] = []
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  readyState = MockWebSocket.CONNECTING
  url: string
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  onclose: (() => void) | null = null
  send = vi.fn()

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.()
  }

  open() {
    this.readyState = MockWebSocket.OPEN
    this.onopen?.()
  }
}

describe('useWebSocketClient', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    MockWebSocket.instances = []
    getAccessTokenMock.mockReset()
    vi.stubGlobal('WebSocket', MockWebSocket)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('does not reconnect after manual disconnect', async () => {
    const { result } = renderHook(() =>
      useWebSocketClient({
        url: 'ws://localhost:8080/ws',
        reconnectDelayMs: 250,
      }),
    )

    expect(MockWebSocket.instances).toHaveLength(1)
    act(() => {
      MockWebSocket.instances[0]?.open()
    })
    expect(result.current.status).toBe('open')

    act(() => {
      result.current.disconnect()
    })
    expect(result.current.status).toBe('closed')

    act(() => {
      vi.advanceTimersByTime(300)
    })

    expect(MockWebSocket.instances).toHaveLength(1)
  })

  it('attaches token query param when access token exists', () => {
    getAccessTokenMock.mockReturnValue('abc123token')

    renderHook(() =>
      useWebSocketClient({
        url: 'ws://localhost:8080/ws',
      }),
    )

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0]?.url).toBe('ws://localhost:8080/ws?token=abc123token')
  })

  it('keeps existing token query param unchanged', () => {
    getAccessTokenMock.mockReturnValue('newtoken')

    renderHook(() =>
      useWebSocketClient({
        url: 'ws://localhost:8080/ws?token=existing',
      }),
    )

    expect(MockWebSocket.instances).toHaveLength(1)
    expect(MockWebSocket.instances[0]?.url).toBe('ws://localhost:8080/ws?token=existing')
  })
})
