import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { Settings, Strategy } from '@/lib/api/types'

import { StrategyConfigEditor } from './strategy-config-editor'

const mockStrategy: Strategy = {
  id: '00000000-0000-0000-0000-000000000001',
  name: 'Test Strategy',
  description: 'A test strategy',
  ticker: 'AAPL',
  market_type: 'stock',
  schedule_cron: '0 9 * * 1-5',
  config: {
    llm_config: {
      provider: 'anthropic',
      deep_think_model: 'claude-3-opus',
      quick_think_model: 'gpt-4o-mini',
    },
    pipeline_config: {
      debate_rounds: 4,
      analysis_timeout_seconds: 120,
      debate_timeout_seconds: 600,
    },
    risk_config: {
      position_size_pct: 20,
      stop_loss_multiplier: 1.5,
      take_profit_multiplier: 2.5,
      min_confidence: 0.7,
    },
    analyst_selection: ['market_analyst', 'news_analyst'],
    prompt_overrides: {
      trader: 'Use custom trader prompt',
    },
  },
  status: 'active',
  skip_next_run: false,
  is_paper: true,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-01-01T00:00:00Z',
}

const mockSettings: Settings = {
  llm: {
    default_provider: 'openai',
    deep_think_model: 'gpt-4o',
    quick_think_model: 'gpt-4o-mini',
    providers: {
      openai: { model: 'gpt-4o', api_key_configured: true },
      anthropic: { model: 'claude-3-opus', api_key_configured: true },
      google: { model: 'gemini-pro', api_key_configured: false },
      openrouter: { model: 'auto', api_key_configured: false },
      xai: { model: 'grok-1', api_key_configured: false },
      ollama: { model: 'llama3', base_url: 'http://localhost:11434' },
    },
  },
  risk: {
    max_position_size_pct: 5,
    max_daily_loss_pct: 2,
    max_drawdown_pct: 10,
    max_open_positions: 5,
    max_total_exposure_pct: 50,
    max_per_market_exposure_pct: 30,
    circuit_breaker_threshold_pct: 5,
    circuit_breaker_cooldown_min: 60,
  },
  system: {
    environment: 'development',
    version: '0.1.0',
    uptime_seconds: 3600,
    connected_brokers: [],
  },
}

describe('StrategyConfigEditor', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders LLM config fields with settings providers', () => {
    render(
      <StrategyConfigEditor
        strategy={mockStrategy}
        onSave={vi.fn()}
        settings={mockSettings}
      />,
    )

    const deepProviderSelect = screen.getByLabelText('Deep Think Provider') as HTMLSelectElement
    const quickProviderSelect = screen.getByLabelText('Quick Think Provider') as HTMLSelectElement

    expect(deepProviderSelect).toBeInTheDocument()
    expect(quickProviderSelect).toBeInTheDocument()
    expect(deepProviderSelect.querySelectorAll('option')).toHaveLength(7)
  })

  it('pre-populates pipeline and risk fields from strategy config', () => {
    render(
      <StrategyConfigEditor
        strategy={mockStrategy}
        onSave={vi.fn()}
        settings={mockSettings}
      />,
    )

    expect(screen.getByLabelText('Research Debate Rounds')).toHaveValue(4)
    expect(screen.getByLabelText('Risk Debate Rounds')).toHaveValue(null)
    expect(screen.getByLabelText('Analysis Timeout (seconds)')).toHaveValue(120)
    expect(screen.getByLabelText('Debate Timeout (seconds)')).toHaveValue(600)
    expect(screen.getByLabelText('Max Position Size %')).toHaveValue(0.2)
    expect(screen.getByLabelText('Stop Loss ATR Multiplier')).toHaveValue(1.5)
    expect(screen.getByLabelText('Take Profit ATR Multiplier')).toHaveValue(2.5)
    expect(screen.getByLabelText('Min Confidence Threshold')).toHaveValue(0.7)
  })

  it('defaults all analysts to checked when no config is present', () => {
    render(
      <StrategyConfigEditor
        strategy={{ ...mockStrategy, config: {} }}
        onSave={vi.fn()}
        settings={mockSettings}
      />,
    )

    expect(screen.getByLabelText('Market Analyst')).toBeChecked()
    expect(screen.getByLabelText('Fundamentals Analyst')).toBeChecked()
    expect(screen.getByLabelText('News Analyst')).toBeChecked()
    expect(screen.getByLabelText('Social Media Analyst')).toBeChecked()
  })

  it('keeps advanced section collapsed by default and expands on click', () => {
    render(
      <StrategyConfigEditor
        strategy={mockStrategy}
        onSave={vi.fn()}
        settings={mockSettings}
      />,
    )

    expect(screen.queryByLabelText('Prompt Overrides (JSON)')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show' }))
    expect(screen.getByLabelText('Prompt Overrides (JSON)')).toBeInTheDocument()
  })

  it('includes new config sections in submitted payload', () => {
    const onSave = vi.fn()

    render(
      <StrategyConfigEditor
        strategy={mockStrategy}
        onSave={onSave}
        settings={mockSettings}
      />,
    )

    fireEvent.change(screen.getByLabelText('Deep Think Provider'), { target: { value: 'anthropic' } })
    fireEvent.change(screen.getByLabelText('Deep Think Model'), { target: { value: 'claude-4' } })
    fireEvent.change(screen.getByLabelText('Quick Think Provider'), { target: { value: 'openai' } })
    fireEvent.change(screen.getByLabelText('Quick Think Model'), { target: { value: 'gpt-5' } })
    fireEvent.change(screen.getByLabelText('Research Debate Rounds'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Risk Debate Rounds'), { target: { value: '2' } })
    fireEvent.change(screen.getByLabelText('Analysis Timeout (seconds)'), { target: { value: '90' } })
    fireEvent.change(screen.getByLabelText('Debate Timeout (seconds)'), { target: { value: '900' } })
    fireEvent.change(screen.getByLabelText('Max Position Size %'), { target: { value: '0.25' } })
    fireEvent.change(screen.getByLabelText('Stop Loss ATR Multiplier'), { target: { value: '1.8' } })
    fireEvent.change(screen.getByLabelText('Take Profit ATR Multiplier'), { target: { value: '2.8' } })
    fireEvent.change(screen.getByLabelText('Min Confidence Threshold'), { target: { value: '0.75' } })

    fireEvent.click(screen.getByLabelText('Fundamentals Analyst'))
    fireEvent.click(screen.getByLabelText('Social Media Analyst'))

    fireEvent.click(screen.getByRole('button', { name: 'Show' }))
    fireEvent.change(screen.getByLabelText('Prompt Overrides (JSON)'), {
      target: { value: '{\n  "trader": "Use concise prompts"\n}' },
    })

    fireEvent.submit(screen.getByTestId('strategy-config-editor').querySelector('form')!)

    expect(onSave).toHaveBeenCalledTimes(1)
    expect(onSave.mock.calls[0][0]).toEqual({
      name: 'Test Strategy',
      description: 'A test strategy',
      ticker: 'AAPL',
      market_type: 'stock',
      schedule_cron: '0 9 * * 1-5',
      status: 'active',
      is_paper: true,
      skip_next_run: false,
      config: {
        llm_config: {
          provider: 'anthropic',
          deep_think_model: 'claude-4',
          quick_think_model: 'gpt-5',
        },
        pipeline_config: {
          debate_rounds: 3,
          analysis_timeout_seconds: 90,
          debate_timeout_seconds: 900,
        },
        risk_config: {
          position_size_pct: 25,
          stop_loss_multiplier: 1.8,
          take_profit_multiplier: 2.8,
          min_confidence: 0.75,
        },
        analyst_selection: ['market_analyst', 'news_analyst', 'fundamentals_analyst', 'social_media_analyst'],
        prompt_overrides: {
          trader: 'Use concise prompts',
        },
      },
    })
  })

  it('blocks submit when no analysts are selected', () => {
    const onSave = vi.fn()

    render(
      <StrategyConfigEditor
        strategy={{ ...mockStrategy, config: {} }}
        onSave={onSave}
        settings={mockSettings}
      />,
    )

    fireEvent.click(screen.getByLabelText('Market Analyst'))
    fireEvent.click(screen.getByLabelText('Fundamentals Analyst'))
    fireEvent.click(screen.getByLabelText('News Analyst'))
    fireEvent.click(screen.getByLabelText('Social Media Analyst'))
    fireEvent.submit(screen.getByTestId('strategy-config-editor').querySelector('form')!)

    expect(onSave).not.toHaveBeenCalled()
    expect(screen.getByText('Select at least one analyst')).toBeInTheDocument()
  })
})
