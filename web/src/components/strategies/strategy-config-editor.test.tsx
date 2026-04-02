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
    llm: {
      deep_think_provider: 'anthropic',
      deep_think_model: 'claude-3-opus',
      quick_think_provider: 'openai',
      quick_think_model: 'gpt-4o-mini',
    },
  },
  is_active: true,
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
  afterEach(cleanup)

  it('renders LLM config fields with settings providers', () => {
    render(
      <StrategyConfigEditor
        strategy={mockStrategy}
        onSave={vi.fn()}
        settings={mockSettings}
      />,
    )

    // Provider dropdowns should have all providers as options
    const deepProviderSelect = screen.getByLabelText('Deep Think Provider') as HTMLSelectElement
    const quickProviderSelect = screen.getByLabelText('Quick Think Provider') as HTMLSelectElement

    expect(deepProviderSelect).toBeInTheDocument()
    expect(quickProviderSelect).toBeInTheDocument()

    // Each dropdown should have "Use global default" + 6 providers
    const deepOptions = deepProviderSelect.querySelectorAll('option')
    expect(deepOptions).toHaveLength(7) // 1 default + 6 providers

    expect(deepOptions[0]).toHaveTextContent('Use global default')
    expect(deepOptions[1]).toHaveTextContent('openai')
    expect(deepOptions[2]).toHaveTextContent('anthropic')
  })

  it('pre-populates LLM fields from strategy config', () => {
    render(
      <StrategyConfigEditor
        strategy={mockStrategy}
        onSave={vi.fn()}
        settings={mockSettings}
      />,
    )

    const deepProviderSelect = screen.getByLabelText('Deep Think Provider') as HTMLSelectElement
    const deepModelInput = screen.getByLabelText('Deep Think Model') as HTMLInputElement
    const quickProviderSelect = screen.getByLabelText('Quick Think Provider') as HTMLSelectElement
    const quickModelInput = screen.getByLabelText('Quick Think Model') as HTMLInputElement

    expect(deepProviderSelect.value).toBe('anthropic')
    expect(deepModelInput.value).toBe('claude-3-opus')
    expect(quickProviderSelect.value).toBe('openai')
    expect(quickModelInput.value).toBe('gpt-4o-mini')
  })

  it('shows global defaults as placeholders', () => {
    const strategyNoLlm: Strategy = {
      ...mockStrategy,
      config: {},
    }

    render(
      <StrategyConfigEditor
        strategy={strategyNoLlm}
        onSave={vi.fn()}
        settings={mockSettings}
      />,
    )

    const deepModelInput = screen.getByLabelText('Deep Think Model') as HTMLInputElement
    const quickModelInput = screen.getByLabelText('Quick Think Model') as HTMLInputElement

    expect(deepModelInput.placeholder).toBe('gpt-4o')
    expect(quickModelInput.placeholder).toBe('gpt-4o-mini')
  })

  it('renders only global default option when no settings provided', () => {
    render(
      <StrategyConfigEditor
        strategy={mockStrategy}
        onSave={vi.fn()}
      />,
    )

    const deepProviderSelect = screen.getByLabelText('Deep Think Provider') as HTMLSelectElement
    const options = deepProviderSelect.querySelectorAll('option')
    expect(options).toHaveLength(1)
    expect(options[0]).toHaveTextContent('Use global default')
  })

  it('includes LLM fields in submitted config', () => {
    const onSave = vi.fn()

    render(
      <StrategyConfigEditor
        strategy={{ ...mockStrategy, config: {} }}
        onSave={onSave}
        settings={mockSettings}
      />,
    )

    // Fill LLM fields
    fireEvent.change(screen.getByLabelText('Deep Think Provider'), { target: { value: 'anthropic' } })
    fireEvent.change(screen.getByLabelText('Deep Think Model'), { target: { value: 'claude-4' } })
    fireEvent.change(screen.getByLabelText('Quick Think Provider'), { target: { value: 'openai' } })
    fireEvent.change(screen.getByLabelText('Quick Think Model'), { target: { value: 'gpt-5' } })

    // Submit
    fireEvent.submit(screen.getByTestId('strategy-config-editor').querySelector('form')!)

    expect(onSave).toHaveBeenCalledTimes(1)
    const payload = onSave.mock.calls[0][0]
    expect(payload.config).toEqual({
      llm: {
        deep_think_provider: 'anthropic',
        deep_think_model: 'claude-4',
        quick_think_provider: 'openai',
        quick_think_model: 'gpt-5',
      },
    })
  })

  it('does not include empty LLM fields in config', () => {
    const onSave = vi.fn()

    render(
      <StrategyConfigEditor
        strategy={{ ...mockStrategy, config: {} }}
        onSave={onSave}
        settings={mockSettings}
      />,
    )

    // Leave all LLM fields empty, just submit
    fireEvent.submit(screen.getByTestId('strategy-config-editor').querySelector('form')!)

    expect(onSave).toHaveBeenCalledTimes(1)
    const payload = onSave.mock.calls[0][0]
    // No llm key should exist when all fields are empty
    expect(payload.config).toEqual({})
  })

  it('merges LLM fields with existing JSON config', () => {
    const onSave = vi.fn()
    const strategyWithExtraConfig: Strategy = {
      ...mockStrategy,
      config: { some_other_key: 'value', llm: { existing_key: 'keep' } },
    }

    render(
      <StrategyConfigEditor
        strategy={strategyWithExtraConfig}
        onSave={onSave}
        settings={mockSettings}
      />,
    )

    // Set deep think provider, clear everything else
    fireEvent.change(screen.getByLabelText('Deep Think Provider'), { target: { value: 'anthropic' } })
    fireEvent.change(screen.getByLabelText('Deep Think Model'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Quick Think Provider'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Quick Think Model'), { target: { value: '' } })

    fireEvent.submit(screen.getByTestId('strategy-config-editor').querySelector('form')!)

    expect(onSave).toHaveBeenCalledTimes(1)
    const payload = onSave.mock.calls[0][0]
    expect(payload.config.some_other_key).toBe('value')
    // LLM should merge: existing_key from JSON + deep_think_provider from structured field
    expect(payload.config.llm.existing_key).toBe('keep')
    expect(payload.config.llm.deep_think_provider).toBe('anthropic')
  })

  it('renders LLM section above JSON textarea', () => {
    const { container } = render(
      <StrategyConfigEditor
        strategy={mockStrategy}
        onSave={vi.fn()}
        settings={mockSettings}
      />,
    )

    const llmHeading = screen.getByText('LLM Configuration')
    const textarea = screen.getByTestId('config-editor-textarea')

    // LLM section should appear before the JSON textarea in DOM order
    const allElements = container.querySelectorAll('h4, textarea')
    const positions = Array.from(allElements).map((el) => el.textContent || el.tagName)
    const llmIndex = positions.indexOf('LLM Configuration')
    const textareaIndex = positions.findIndex((_, i) => allElements[i] === textarea)

    expect(llmHeading).toBeInTheDocument()
    expect(llmIndex).toBeLessThan(textareaIndex)
  })
})
