import { parseContract } from '@/shared/api/contract'
import { websocketCommandSchema, websocketEventEnvelopeSchema } from '@/shared/api/schemas'
import type { UUID } from '@/shared/types/primitives'
import type { WebSocketClientCommand, WebSocketEventEnvelope } from '@/shared/types/websocket'
import { buildWebSocketEvent } from '@/test/fixtures/builders'

export type MockWebSocketState = 'closed' | 'connecting' | 'open'

export type MockWebSocketListener = (event: WebSocketEventEnvelope) => void
export type MockWebSocketStateListener = (state: MockWebSocketState) => void

export class MockRealtimeSocket {
  private eventListeners = new Set<MockWebSocketListener>()
  private stateListeners = new Set<MockWebSocketStateListener>()
  private subscriptions = {
    all: false,
    polymarket: false,
    strategyIds: new Set<UUID>(),
    runIds: new Set<UUID>(),
  }

  state: MockWebSocketState = 'closed'
  readonly sentCommands: WebSocketClientCommand[] = []

  connect() {
    this.setState('open')
  }

  disconnect() {
    this.setState('closed')
  }

  reconnect() {
    this.disconnect()
    this.connect()
    for (const command of this.sentCommands) {
      this.applyCommand(command)
    }
  }

  send(command: WebSocketClientCommand) {
    const parsed = parseContract('WebSocket client command', websocketCommandSchema, command)
    this.sentCommands.push(parsed)
    this.applyCommand(parsed)
    return { status: 'ok' as const, action: parsed.action }
  }

  onEvent(listener: MockWebSocketListener) {
    this.eventListeners.add(listener)
    return () => this.eventListeners.delete(listener)
  }

  onState(listener: MockWebSocketStateListener) {
    this.stateListeners.add(listener)
    return () => this.stateListeners.delete(listener)
  }

  emit(event: WebSocketEventEnvelope) {
    const parsed = parseContract('WebSocket event envelope', websocketEventEnvelopeSchema, event)
    if (this.state !== 'open') return false
    if (!this.matchesSubscriptions(parsed)) return false
    for (const listener of this.eventListeners) {
      listener(parsed)
    }
    return true
  }

  emitBurst(events: WebSocketEventEnvelope[]) {
    return events.map((event) => this.emit(event))
  }

  emitOutOfOrder(events: WebSocketEventEnvelope[]) {
    return this.emitBurst([...events].reverse())
  }

  emitUnknownEvent(overrides: Partial<WebSocketEventEnvelope> = {}) {
    return this.emit(buildWebSocketEvent({ type: 'unknown_fixture_event', ...overrides }))
  }

  getSubscriptionSnapshot() {
    return {
      all: this.subscriptions.all,
      polymarket: this.subscriptions.polymarket,
      strategyIds: [...this.subscriptions.strategyIds],
      runIds: [...this.subscriptions.runIds],
    }
  }

  private setState(next: MockWebSocketState) {
    this.state = next
    for (const listener of this.stateListeners) {
      listener(next)
    }
  }

  private applyCommand(command: WebSocketClientCommand) {
    switch (command.action) {
      case 'subscribe':
        command.strategy_ids?.forEach((id) => this.subscriptions.strategyIds.add(id))
        command.run_ids?.forEach((id) => this.subscriptions.runIds.add(id))
        break
      case 'unsubscribe':
        command.strategy_ids?.forEach((id) => this.subscriptions.strategyIds.delete(id))
        command.run_ids?.forEach((id) => this.subscriptions.runIds.delete(id))
        break
      case 'subscribe_all':
        this.subscriptions.all = true
        break
      case 'unsubscribe_all':
        this.subscriptions.all = false
        this.subscriptions.strategyIds.clear()
        this.subscriptions.runIds.clear()
        break
      case 'subscribe_polymarket':
        this.subscriptions.polymarket = true
        break
      case 'unsubscribe_polymarket':
        this.subscriptions.polymarket = false
        break
    }
  }

  private matchesSubscriptions(event: WebSocketEventEnvelope) {
    if (this.subscriptions.all) return true
    if (event.strategy_id && this.subscriptions.strategyIds.has(event.strategy_id)) return true
    if (event.run_id && this.subscriptions.runIds.has(event.run_id)) return true
    if (event.type.startsWith('polymarket_') && this.subscriptions.polymarket) return true
    return false
  }
}

export function createP0WebSocketEvents() {
  return [
    buildWebSocketEvent({ type: 'pipeline_start' }),
    buildWebSocketEvent({ type: 'agent_decision', data: { agent: 'fixture', mode: 'paper' } }),
    buildWebSocketEvent({ type: 'signal', data: { signal: 'hold' } }),
    buildWebSocketEvent({ type: 'order_submitted' }),
    buildWebSocketEvent({ type: 'order_filled' }),
    buildWebSocketEvent({ type: 'position_update' }),
    buildWebSocketEvent({ type: 'circuit_breaker', data: { state: 'open' } }),
    buildWebSocketEvent({ type: 'error', data: { message: 'fixture error' } }),
    buildWebSocketEvent({ type: 'pipeline_health', data: { healthy: true } }),
  ]
}
