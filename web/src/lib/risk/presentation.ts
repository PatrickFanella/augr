import { AlertTriangle, CheckCircle2, XCircle } from 'lucide-react'

import type {
  CircuitBreakerPhase,
  CircuitBreakerStatus,
  KillSwitchMechanism,
  KillSwitchStatus,
  MarketType,
  RiskCockpitExposure,
  RiskStatus,
} from '@/lib/api/types'

export const RISK_COCKPIT_MARKET_ORDER: RiskCockpitExposure['market_type'][] = ['stock', 'crypto', 'options', 'polymarket', 'kalshi']

export const RISK_COCKPIT_MARKET_LABELS: Record<RiskCockpitExposure['market_type'], string> = {
  stock: 'Stock',
  crypto: 'Crypto',
  options: 'Options',
  polymarket: 'Polymarket',
  kalshi: 'Kalshi',
}

export const RISK_MARKET_KILL_SWITCH_ORDER: MarketType[] = ['stock', 'crypto', 'polymarket', 'kalshi']

export const RISK_MARKET_KILL_SWITCH_LABELS: Record<MarketType, string> = {
  stock: 'Stocks',
  crypto: 'Crypto',
  polymarket: 'Polymarket',
  options: 'Options',
  kalshi: 'Kalshi',
}

const RISK_STATUS_DISPLAY: Record<RiskStatus, { label: string; variant: 'success' | 'warning' | 'destructive' }> = {
  normal: { label: 'Normal', variant: 'success' },
  warning: { label: 'Warning', variant: 'warning' },
  breached: { label: 'Breached', variant: 'destructive' },
}

const RISK_STATUS_ICONS = {
  normal: CheckCircle2,
  warning: AlertTriangle,
  breached: XCircle,
} as const

const CIRCUIT_BREAKER_DISPLAY: Record<CircuitBreakerPhase, { label: string; variant: 'success' | 'warning' | 'destructive' }> = {
  open: { label: 'Open', variant: 'success' },
  tripped: { label: 'Tripped', variant: 'destructive' },
  cooldown: { label: 'Cooldown', variant: 'warning' },
}

export function getRiskStatusDisplay(status: RiskStatus) {
  return {
    ...RISK_STATUS_DISPLAY[status],
    icon: RISK_STATUS_ICONS[status],
  }
}

export function getCircuitBreakerDisplay(status: CircuitBreakerStatus) {
  return CIRCUIT_BREAKER_DISPLAY[status.state]
}

export function getCircuitBreakerSummaryDisplay(tripped: boolean) {
  return {
    badgeLabel: tripped ? 'Circuit breaker tripped' : 'Circuit breaker clear',
    badgeVariant: tripped ? ('warning' as const) : ('success' as const),
  }
}

export function getKillSwitchDisplay(status: KillSwitchStatus) {
  const active = status.active

  return {
    badgeLabel: active ? 'Trading halted' : 'Trading enabled',
    badgeVariant: active ? ('destructive' as const) : ('success' as const),
    description: active
      ? (status.reason && status.reason.trim()) || 'All orders are blocked.'
      : 'The engine can submit orders normally.',
    mechanismText: status.mechanisms?.length ? status.mechanisms.map(formatKillSwitchMechanism).join(', ') : '',
  }
}

export function formatKillSwitchMechanism(mechanism: KillSwitchMechanism) {
  switch (mechanism) {
    case 'api_toggle':
      return 'API toggle'
    case 'file_flag':
      return 'File flag'
    case 'env_var':
      return 'Environment variable'
    case 'unknown':
      return 'Unknown'
  }
}

export function getCockpitMarketLabel(marketType: RiskCockpitExposure['market_type']) {
  return RISK_COCKPIT_MARKET_LABELS[marketType]
}

export function getMarketKillSwitchLabel(marketType: MarketType) {
  return RISK_MARKET_KILL_SWITCH_LABELS[marketType]
}
