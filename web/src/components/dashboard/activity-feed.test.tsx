import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ActivityFeed } from '@/components/dashboard/activity-feed'

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

describe('ActivityFeed', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    MockWebSocket.instances = []
    vi.stubGlobal('WebSocket', MockWebSocket)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('shows empty state when no events received', () => {
    render(<ActivityFeed />)

    expect(screen.getByTestId('activity-feed')).toBeInTheDocument()
    expect(screen.getByTestId('activity-feed-empty')).toBeInTheDocument()
  })

  it('shows connected badge after WebSocket opens', async () => {
    render(<ActivityFeed />)

    const ws = MockWebSocket.instances[0]
    expect(ws).toBeDefined()

    await vi.waitFor(() => {
      ws?.open()
      expect(screen.getByText('Connected')).toBeInTheDocument()
    })
  })

  it('renders pipeline health websocket events with a human-friendly label', async () => {
    render(<ActivityFeed />)

    const ws = MockWebSocket.instances[0]
    expect(ws).toBeDefined()

    await act(async () => {
      ws?.open()
      ws?.onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({
            type: 'pipeline_health',
            strategy_id: '11111111-1111-1111-1111-111111111111',
            timestamp: '2026-04-21T13:45:00.000Z',
          }),
        }),
      )
    })

    expect(screen.getByText('Pipeline health')).toBeInTheDocument()
  })
})
