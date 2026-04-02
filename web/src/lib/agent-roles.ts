import type { AgentRole } from '@/lib/api/types'

export const AGENT_ROLE_OPTIONS: AgentRole[] = [
  'market_analyst',
  'fundamentals_analyst',
  'news_analyst',
  'social_media_analyst',
  'bull_researcher',
  'bear_researcher',
  'trader',
  'invest_judge',
  'risk_manager',
  'aggressive_analyst',
  'conservative_analyst',
  'neutral_analyst',
  'aggressive_risk',
  'conservative_risk',
  'neutral_risk',
]

export function formatAgentRole(role: AgentRole) {
  return role.replaceAll('_', ' ')
}
