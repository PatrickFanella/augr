import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { CreateStrategyDialog } from './create-strategy-dialog'

describe('CreateStrategyDialog', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders structured config fields and keeps advanced collapsed by default', () => {
    render(<CreateStrategyDialog open onOpenChange={vi.fn()} onSubmit={vi.fn()} />)

    expect(screen.getByLabelText('Research Debate Rounds')).toBeInTheDocument()
    expect(screen.getByLabelText('Risk Debate Rounds')).toBeInTheDocument()
    expect(screen.getByLabelText('Analysis Timeout (seconds)')).toBeInTheDocument()
    expect(screen.getByLabelText('Debate Timeout (seconds)')).toBeInTheDocument()
    expect(screen.getByLabelText('Max Position Size %')).toBeInTheDocument()
    expect(screen.getByLabelText('Stop Loss ATR Multiplier')).toBeInTheDocument()
    expect(screen.getByLabelText('Take Profit ATR Multiplier')).toBeInTheDocument()
    expect(screen.getByLabelText('Min Confidence Threshold')).toBeInTheDocument()

    expect(screen.getByLabelText('Market Analyst')).toBeChecked()
    expect(screen.getByLabelText('Fundamentals Analyst')).toBeChecked()
    expect(screen.getByLabelText('News Analyst')).toBeChecked()
    expect(screen.getByLabelText('Social Media Analyst')).toBeChecked()

    expect(screen.queryByLabelText('Prompt Overrides (JSON)')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Show' }))
    expect(screen.getByLabelText('Prompt Overrides (JSON)')).toBeInTheDocument()
  })

  it('includes structured config sections in submitted payload', () => {
    const onSubmit = vi.fn()

    render(<CreateStrategyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} />)

    fireEvent.change(screen.getByTestId('strategy-name-input'), { target: { value: 'Momentum' } })
    fireEvent.change(screen.getByTestId('strategy-ticker-input'), { target: { value: 'aapl' } })
    fireEvent.change(screen.getByLabelText('Description'), { target: { value: 'Breakout strategy' } })
    fireEvent.change(screen.getByLabelText('Schedule (cron)'), { target: { value: '0 9 * * 1-5' } })

    fireEvent.change(screen.getByLabelText('Research Debate Rounds'), { target: { value: '4' } })
    fireEvent.change(screen.getByLabelText('Risk Debate Rounds'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Analysis Timeout (seconds)'), { target: { value: '120' } })
    fireEvent.change(screen.getByLabelText('Debate Timeout (seconds)'), { target: { value: '600' } })
    fireEvent.change(screen.getByLabelText('Max Position Size %'), { target: { value: '0.25' } })
    fireEvent.change(screen.getByLabelText('Stop Loss ATR Multiplier'), { target: { value: '1.5' } })
    fireEvent.change(screen.getByLabelText('Take Profit ATR Multiplier'), { target: { value: '2.5' } })
    fireEvent.change(screen.getByLabelText('Min Confidence Threshold'), { target: { value: '0.7' } })

    fireEvent.click(screen.getByLabelText('Fundamentals Analyst'))
    fireEvent.click(screen.getByRole('button', { name: 'Show' }))
    fireEvent.change(screen.getByLabelText('Prompt Overrides (JSON)'), {
      target: { value: '{\n  "trader": "Use concise prompts"\n}' },
    })

    fireEvent.submit(screen.getByRole('button', { name: 'Create strategy' }).closest('form')!)

    expect(onSubmit).toHaveBeenCalledTimes(1)
    expect(onSubmit.mock.calls[0][0]).toEqual({
      name: 'Momentum',
      description: 'Breakout strategy',
      ticker: 'AAPL',
      market_type: 'stock',
      schedule_cron: '0 9 * * 1-5',
      status: 'inactive',
      is_paper: true,
      config: {
        pipeline_config: {
          debate_rounds: 4,
          analysis_timeout_seconds: 120,
          debate_timeout_seconds: 600,
        },
        risk_config: {
          position_size_pct: 25,
          stop_loss_multiplier: 1.5,
          take_profit_multiplier: 2.5,
          min_confidence: 0.7,
        },
        analyst_selection: ['market_analyst', 'news_analyst', 'social_media_analyst'],
        prompt_overrides: {
          trader: 'Use concise prompts',
        },
      },
    })
  })

  it('blocks submit when no analysts are selected', () => {
    const onSubmit = vi.fn()

    render(<CreateStrategyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} />)

    fireEvent.change(screen.getByTestId('strategy-name-input'), { target: { value: 'Momentum' } })
    fireEvent.change(screen.getByTestId('strategy-ticker-input'), { target: { value: 'AAPL' } })
    fireEvent.click(screen.getByLabelText('Market Analyst'))
    fireEvent.click(screen.getByLabelText('Fundamentals Analyst'))
    fireEvent.click(screen.getByLabelText('News Analyst'))
    fireEvent.click(screen.getByLabelText('Social Media Analyst'))

    fireEvent.submit(screen.getByRole('button', { name: 'Create strategy' }).closest('form')!)

    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getByText('Select at least one analyst')).toBeInTheDocument()
  })

  it('blocks submit for invalid prompt override JSON and numeric ranges', () => {
    const onSubmit = vi.fn()

    render(<CreateStrategyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} />)

    fireEvent.change(screen.getByTestId('strategy-name-input'), { target: { value: 'Momentum' } })
    fireEvent.change(screen.getByTestId('strategy-ticker-input'), { target: { value: 'AAPL' } })
    fireEvent.change(screen.getByLabelText('Research Debate Rounds'), { target: { value: '11' } })

    fireEvent.submit(screen.getByRole('button', { name: 'Create strategy' }).closest('form')!)

    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getByText('Debate rounds must be between 1 and 10')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Research Debate Rounds'), { target: { value: '4' } })
    fireEvent.click(screen.getByRole('button', { name: 'Show' }))
    fireEvent.change(screen.getByLabelText('Prompt Overrides (JSON)'), { target: { value: '{invalid' } })
    fireEvent.submit(screen.getByRole('button', { name: 'Create strategy' }).closest('form')!)

    expect(onSubmit).not.toHaveBeenCalled()
    expect(screen.getByText('Prompt overrides must be valid JSON')).toBeInTheDocument()
  })
})
