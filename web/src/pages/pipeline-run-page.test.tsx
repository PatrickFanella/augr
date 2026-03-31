import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PipelineRunPage } from '@/pages/pipeline-run-page'

const runId = '00000000-0000-0000-0000-000000000099'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/runs/${runId}`]}>
        <Routes>
          <Route path="runs/:id" element={children} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

const mockRun = {
  id: runId,
  strategy_id: '00000000-0000-0000-0000-000000000001',
  ticker: 'AAPL',
  trade_date: '2025-06-15',
  status: 'completed' as const,
  signal: 'buy' as const,
  started_at: '2025-06-15T09:00:00Z',
  completed_at: '2025-06-15T09:05:00Z',
}

const mockDecisions = {
  data: [
    {
      id: 'd1',
      pipeline_run_id: runId,
      agent_role: 'market_analyst',
      phase: 'analysis',
      output_text: 'Market conditions are bullish with strong momentum.',
      prompt_tokens: 500,
      completion_tokens: 200,
      latency_ms: 1200,
      input_summary: 'Analyze market conditions for AAPL',
      llm_model: 'gpt-4',
      created_at: '2025-06-15T09:00:10Z',
    },
    {
      id: 'd2',
      pipeline_run_id: runId,
      agent_role: 'fundamentals_analyst',
      phase: 'analysis',
      output_text: 'Strong fundamentals with growing revenue.',
      latency_ms: 1100,
      created_at: '2025-06-15T09:00:11Z',
    },
    {
      id: 'd3',
      pipeline_run_id: runId,
      agent_role: 'news_analyst',
      phase: 'analysis',
      output_text: 'Positive news sentiment detected.',
      latency_ms: 900,
      created_at: '2025-06-15T09:00:12Z',
    },
    {
      id: 'd4',
      pipeline_run_id: runId,
      agent_role: 'social_media_analyst',
      phase: 'analysis',
      output_text: 'Social media buzz is positive.',
      latency_ms: 800,
      created_at: '2025-06-15T09:00:13Z',
    },
    {
      id: 'd5',
      pipeline_run_id: runId,
      agent_role: 'bull_researcher',
      phase: 'research_debate',
      round_number: 1,
      output_text: 'Bull case: Strong growth trajectory ahead.',
      latency_ms: 1500,
      created_at: '2025-06-15T09:01:00Z',
    },
    {
      id: 'd6',
      pipeline_run_id: runId,
      agent_role: 'bear_researcher',
      phase: 'research_debate',
      round_number: 1,
      output_text: 'Bear case: Valuation is stretched.',
      latency_ms: 1400,
      created_at: '2025-06-15T09:01:01Z',
    },
    {
      id: 'd7',
      pipeline_run_id: runId,
      agent_role: 'trader',
      phase: 'trading',
      output_text: 'Plan: Buy 100 shares at market open with stop loss at 5%.',
      latency_ms: 1000,
      created_at: '2025-06-15T09:02:00Z',
    },
    {
      id: 'd8',
      pipeline_run_id: runId,
      agent_role: 'aggressive_risk',
      phase: 'risk_debate',
      round_number: 1,
      output_text: 'Risk is acceptable, position size can be increased.',
      latency_ms: 800,
      created_at: '2025-06-15T09:03:00Z',
    },
    {
      id: 'd9',
      pipeline_run_id: runId,
      agent_role: 'conservative_risk',
      phase: 'risk_debate',
      round_number: 1,
      output_text: 'Reduce position size by 50% due to volatility.',
      latency_ms: 900,
      created_at: '2025-06-15T09:03:01Z',
    },
    {
      id: 'd10',
      pipeline_run_id: runId,
      agent_role: 'neutral_risk',
      phase: 'risk_debate',
      round_number: 1,
      output_text: 'Current position size is appropriate.',
      latency_ms: 700,
      created_at: '2025-06-15T09:03:02Z',
    },
    {
      id: 'd11',
      pipeline_run_id: runId,
      agent_role: 'invest_judge',
      phase: 'risk_debate',
      output_text: 'Final decision: BUY with confidence: 78%.',
      output_structured: { confidence: 78 },
      latency_ms: 600,
      created_at: '2025-06-15T09:04:00Z',
    },
  ],
  limit: 100,
  offset: 0,
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('PipelineRunPage', () => {
  it('renders pipeline run visualization on successful fetch', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/decisions')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => mockDecisions })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => mockRun })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PipelineRunPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('pipeline-run-page')).toBeInTheDocument()
    expect(screen.getByText('AAPL — Pipeline Run')).toBeInTheDocument()
    expect(screen.getByTestId('phase-progress')).toBeInTheDocument()
    expect(screen.getByTestId('analyst-cards')).toBeInTheDocument()
    expect(screen.getAllByTestId('debate-view')).toHaveLength(2)
    expect(screen.getByTestId('trader-plan')).toBeInTheDocument()
    expect(screen.getByTestId('final-signal')).toBeInTheDocument()
  })

  it('shows error state when run fetch fails', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new Error('Network error'))
    vi.stubGlobal('fetch', fetchMock)

    render(<PipelineRunPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('pipeline-run-error')).toBeInTheDocument()
  })

  it('displays analyst cards with completed decisions', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/decisions')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => mockDecisions })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => mockRun })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PipelineRunPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('analyst-card-market_analyst')).toBeInTheDocument()
    expect(screen.getByTestId('analyst-card-fundamentals_analyst')).toBeInTheDocument()
    expect(screen.getByTestId('analyst-card-news_analyst')).toBeInTheDocument()
    expect(screen.getByTestId('analyst-card-social_media_analyst')).toBeInTheDocument()
    expect(screen.getByText(/Market conditions are bullish/)).toBeInTheDocument()
  })

  it('opens decision inspector when clicking an analyst card', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/decisions')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => mockDecisions })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => mockRun })
    })
    vi.stubGlobal('fetch', fetchMock)

    const user = userEvent.setup()
    render(<PipelineRunPage />, { wrapper: Wrapper })

    const card = await screen.findByTestId('analyst-card-market_analyst')
    await user.click(card)

    const inspector = screen.getByTestId('decision-inspector')
    expect(inspector).toBeInTheDocument()
    expect(inspector).toHaveTextContent('Market Analyst')
    expect(screen.getByTestId('inspector-response')).toHaveTextContent(
      'Market conditions are bullish with strong momentum.',
    )
  })

  it('closes decision inspector when close button is clicked', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/decisions')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => mockDecisions })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => mockRun })
    })
    vi.stubGlobal('fetch', fetchMock)

    const user = userEvent.setup()
    render(<PipelineRunPage />, { wrapper: Wrapper })

    const card = await screen.findByTestId('analyst-card-market_analyst')
    await user.click(card)

    expect(screen.getByTestId('decision-inspector')).toBeInTheDocument()

    await user.click(screen.getByTestId('inspector-close'))
    expect(screen.queryByTestId('decision-inspector')).not.toBeInTheDocument()
  })

  it('shows confidence score in final signal', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/decisions')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => mockDecisions })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => mockRun })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PipelineRunPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('confidence-score')).toHaveTextContent('78% confidence')
  })

  it('shows buy signal badge', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/decisions')) {
        return Promise.resolve({ ok: true, status: 200, json: async () => mockDecisions })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => mockRun })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PipelineRunPage />, { wrapper: Wrapper })

    expect(await screen.findByText('buy')).toBeInTheDocument()
  })

  it('renders safely when the decisions data array is null', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/decisions')) {
        return Promise.resolve({
          ok: true,
          status: 200,
          json: async () => ({ ...mockDecisions, data: null }),
        })
      }
      return Promise.resolve({ ok: true, status: 200, json: async () => mockRun })
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<PipelineRunPage />, { wrapper: Wrapper })

    expect(await screen.findByTestId('pipeline-run-page')).toBeInTheDocument()
    expect(screen.getAllByText('Waiting for result…')).toHaveLength(4)
    expect(screen.getAllByText('Waiting for debate to begin…')).toHaveLength(2)
  })
})
