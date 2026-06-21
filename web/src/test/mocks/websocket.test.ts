import { describe, expect, it } from 'vitest'

import { fixtureId } from '@/test/fixtures'
import { buildWebSocketEvent } from '@/test/fixtures/builders'
import { createP0WebSocketEvents, MockRealtimeSocket } from '@/test/mocks/websocket'

describe('MockRealtimeSocket', () => {
  it('connects, disconnects, and reconnects', () => {
    const socket = new MockRealtimeSocket()
    const states: string[] = []
    socket.onState((state) => states.push(state))

    socket.connect()
    socket.disconnect()
    socket.reconnect()

    expect(states).toEqual(['open', 'closed', 'closed', 'open'])
    expect(socket.state).toBe('open')
  })

  it('tracks subscription commands and emits matching bursts', () => {
    const socket = new MockRealtimeSocket()
    const received: string[] = []
    socket.onEvent((event) => received.push(event.type))
    socket.connect()
    socket.send({ action: 'subscribe', run_ids: [fixtureId(20)] })

    const results = socket.emitBurst(createP0WebSocketEvents())

    expect(results.every(Boolean)).toBe(true)
    expect(received).toContain('pipeline_start')
    expect(socket.getSubscriptionSnapshot().runIds).toEqual([fixtureId(20)])
  })

  it('supports out-of-order events', () => {
    const socket = new MockRealtimeSocket()
    const received: string[] = []
    socket.onEvent((event) => received.push(event.type))
    socket.connect()
    socket.send({ action: 'subscribe_all' })

    socket.emitOutOfOrder([
      buildWebSocketEvent({ type: 'pipeline_start' }),
      buildWebSocketEvent({ type: 'order_filled' }),
    ])

    expect(received).toEqual(['order_filled', 'pipeline_start'])
  })

  it('supports unknown event types', () => {
    const socket = new MockRealtimeSocket()
    const received: string[] = []
    socket.onEvent((event) => received.push(event.type))
    socket.connect()
    socket.send({ action: 'subscribe_all' })

    expect(socket.emitUnknownEvent()).toBe(true)
    expect(received).toEqual(['unknown_fixture_event'])
  })

  it('does not emit while disconnected', () => {
    const socket = new MockRealtimeSocket()
    socket.send({ action: 'subscribe_all' })
    expect(socket.emit(buildWebSocketEvent())).toBe(false)
  })
})
