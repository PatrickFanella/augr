import type { ISODate, RawJson, UUID } from '@/shared/types/primitives'

export const websocketEventTypes = [
  'pipeline_start',
  'agent_decision',
  'debate_round',
  'signal',
  'order_submitted',
  'order_filled',
  'position_update',
  'circuit_breaker',
  'error',
  'pipeline_health',
  'polymarket_whale_trade',
  'polymarket_price_move',
  'polymarket_account_tracked',
] as const

export const websocketClientActions = [
  'subscribe',
  'unsubscribe',
  'subscribe_all',
  'unsubscribe_all',
  'subscribe_polymarket',
  'unsubscribe_polymarket',
] as const

export type KnownWebSocketEventType = (typeof websocketEventTypes)[number]
export type WebSocketEventType = KnownWebSocketEventType | (string & {})
export type WebSocketClientAction = (typeof websocketClientActions)[number]

export type WebSocketClientCommand =
  | { action: 'subscribe'; strategy_ids?: UUID[]; run_ids?: UUID[] }
  | { action: 'unsubscribe'; strategy_ids?: UUID[]; run_ids?: UUID[] }
  | { action: 'subscribe_all' }
  | { action: 'unsubscribe_all' }
  | { action: 'subscribe_polymarket' }
  | { action: 'unsubscribe_polymarket' }

export type WebSocketEventEnvelope = {
  type: WebSocketEventType
  strategy_id?: UUID
  run_id?: UUID
  data?: RawJson
  timestamp: ISODate
}

export type WebSocketAck = {
  status: 'ok'
  action: WebSocketClientAction
}

export type WebSocketErrorMessage = {
  type: 'error'
  error: string
}
