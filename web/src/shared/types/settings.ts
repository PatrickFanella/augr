import type { RiskSettings } from '@/shared/types/domain'

export type LLMProviderResponse = {
  api_key_configured: boolean
  api_key_last4?: string
  base_url?: string
  model: string
}

export type SettingsResponse = {
  llm: {
    default_provider: string
    deep_think_model: string
    quick_think_model: string
    providers: {
      openai: LLMProviderResponse
      anthropic: LLMProviderResponse
      google: LLMProviderResponse
      openrouter: LLMProviderResponse
      xai: LLMProviderResponse
      ollama: LLMProviderResponse
    }
  }
  risk: RiskSettings
  system: {
    environment: string
    version: string
    build_commit?: string
    build_time?: string
    current_schema_version: number
    required_schema_version: number
    schema_status: string
    uptime_seconds: number
    connected_brokers: Array<{
      name: string
      paper_mode: boolean
      configured: boolean
      data_environment?: 'demo' | 'live' | 'unknown'
      data_source_url?: string
    }>
  }
}
