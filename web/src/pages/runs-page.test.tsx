import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { RunsPage } from '@/pages/runs-page';
import type { PipelineRun } from '@/lib/api/types';

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
      },
    },
  });

  return (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={['/runs']}>
        <Routes>
          <Route path="runs" element={children} />
          <Route
            path="runs/:id"
            element={<div data-testid="run-detail-route">Run detail route</div>}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const strategies = [
  {
    id: '00000000-0000-0000-0000-000000000001',
    name: 'AAPL Momentum',
    ticker: 'AAPL',
    market_type: 'stock',
    is_active: true,
    is_paper: false,
    config: {},
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
  {
    id: '00000000-0000-0000-0000-000000000002',
    name: 'BTC Swing',
    ticker: 'BTCUSD',
    market_type: 'crypto',
    is_active: true,
    is_paper: true,
    config: {},
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  },
];

const baseRun: PipelineRun = {
  id: '10000000-0000-0000-0000-000000000001',
  strategy_id: strategies[0].id,
  ticker: 'AAPL',
  trade_date: '2025-01-03',
  status: 'completed',
  signal: 'buy',
  started_at: '2025-01-03T09:00:00Z',
  completed_at: '2025-01-03T09:01:00Z',
};

function createStrategyResponse() {
  return {
    ok: true,
    status: 200,
    json: async () => ({ data: strategies, total: strategies.length, limit: 500, offset: 0 }),
  };
}

function createRunsResponse(data: (typeof baseRun)[], total = data.length, offset = 0) {
  return {
    ok: true,
    status: 200,
    json: async () => ({ data, total, limit: 21, offset }),
  };
}

describe('RunsPage', () => {
  it('renders filters and populates the strategy dropdown', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockResolvedValueOnce(createRunsResponse([baseRun], 1));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    expect(await screen.findByTestId('runs-table')).toBeInTheDocument();
    expect(screen.getByLabelText(/strategy/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/status/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/from date/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/to date/i)).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'AAPL Momentum (AAPL)' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'BTC Swing (BTCUSD)' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Running' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Completed' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Failed' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Cancelled' })).toBeInTheDocument();
  });

  it('applies filters through listRuns and resets pagination before fetching', async () => {
    const secondPageRuns = Array.from({ length: 21 }, (_, index) => ({
      ...baseRun,
      id: `10000000-0000-0000-0000-000000000${String(index + 10).padStart(3, '0')}`,
      ticker: `RUN${index + 1}`,
      started_at: `2025-01-${String((index % 9) + 1).padStart(2, '0')}T09:00:00Z`,
    }));

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockResolvedValueOnce(createRunsResponse(secondPageRuns, 40))
      .mockResolvedValueOnce(createRunsResponse([baseRun], 1, 20))
      .mockResolvedValueOnce(createRunsResponse([baseRun], 1));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    expect(await screen.findByText('RUN1')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /next/i }));
    expect(await screen.findByText('AAPL')).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/strategy/i), {
      target: { value: strategies[0].id },
    });
    fireEvent.change(screen.getByLabelText(/status/i), {
      target: { value: 'completed' },
    });
    fireEvent.change(screen.getByLabelText(/from date/i), {
      target: { value: '2025-01-01' },
    });
    fireEvent.change(screen.getByLabelText(/to date/i), {
      target: { value: '2025-01-31' },
    });
    fireEvent.click(screen.getByTestId('apply-run-filters'));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));

    const requestUrl = new URL(fetchMock.mock.calls[3][0].toString());
    expect(requestUrl.pathname).toBe('/api/v1/runs');
    expect(requestUrl.searchParams.get('strategy_id')).toBe(strategies[0].id);
    expect(requestUrl.searchParams.get('status')).toBe('completed');
    expect(requestUrl.searchParams.get('start_date')).toBe('2025-01-01T00:00:00.000Z');
    expect(requestUrl.searchParams.get('end_date')).toBe('2025-01-31T23:59:59.999Z');
    expect(requestUrl.searchParams.get('offset')).toBe('0');
    expect(requestUrl.searchParams.get('limit')).toBe('21');
  });

  it('clears filters and shows an empty state when nothing matches', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockResolvedValueOnce(createRunsResponse([baseRun], 1))
      .mockResolvedValueOnce(createRunsResponse([], 0))
      .mockResolvedValueOnce(createRunsResponse([baseRun], 1));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    expect(await screen.findByText('AAPL')).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/status/i), {
      target: { value: 'failed' },
    });
    fireEvent.click(screen.getByTestId('apply-run-filters'));

    expect(await screen.findByTestId('runs-empty')).toHaveTextContent(
      'No runs matched the current filters.',
    );

    fireEvent.click(screen.getByRole('button', { name: /clear/i }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));

    const requestUrl = new URL(fetchMock.mock.calls[3][0].toString());
    expect(requestUrl.searchParams.get('status')).toBeNull();
    expect(requestUrl.searchParams.get('offset')).toBe('0');
    expect(await screen.findByText('AAPL')).toBeInTheDocument();
  });

  it('navigates to the run detail route when a row is clicked', async () => {
    const user = userEvent.setup();
    const runId = baseRun.id;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockResolvedValueOnce(createRunsResponse([{ ...baseRun, id: runId }], 1));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    const row = await screen.findByTestId(`run-row-${runId}`);
    const link = screen.getByTestId(`run-link-${runId}`);

    expect(row).toHaveClass('cursor-pointer');
    expect(row).toHaveClass('hover:bg-accent/45');
    expect(link).toHaveClass('cursor-pointer');

    await user.click(row);

    expect(await screen.findByTestId('run-detail-route')).toBeInTheDocument();
  });

  it('navigates to the run detail route when the row link is activated with Enter', async () => {
    const user = userEvent.setup();
    const runId = baseRun.id;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockResolvedValueOnce(createRunsResponse([{ ...baseRun, id: runId }], 1));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    const link = await screen.findByTestId(`run-link-${runId}`);
    link.focus();

    expect(link).toHaveFocus();

    await user.keyboard('{Enter}');

    expect(await screen.findByTestId('run-detail-route')).toBeInTheDocument();
  });

  it('navigates to the run detail route when the row is activated with Enter', async () => {
    const user = userEvent.setup();
    const runId = baseRun.id;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockResolvedValueOnce(createRunsResponse([{ ...baseRun, id: runId }], 1));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    const row = await screen.findByTestId(`run-row-${runId}`);
    row.focus();

    expect(row).toHaveFocus();

    await user.keyboard('{Enter}');

    expect(await screen.findByTestId('run-detail-route')).toBeInTheDocument();
  });

  it('navigates to the run detail route when the row is activated with Space', async () => {
    const user = userEvent.setup();
    const runId = baseRun.id;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockResolvedValueOnce(createRunsResponse([{ ...baseRun, id: runId }], 1));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    const row = await screen.findByTestId(`run-row-${runId}`);
    row.focus();

    expect(row).toHaveFocus();

    await user.keyboard(' ');

    expect(await screen.findByTestId('run-detail-route')).toBeInTheDocument();
  });

  it('shows error state when fetch fails', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockRejectedValueOnce(new Error('Network error'));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    expect(await screen.findByTestId('runs-error')).toBeInTheDocument();
    expect(
      screen.getByText('Unable to load runs. Start the API server to see live data.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });

  it('renders computed duration and running placeholder', async () => {
    const runningRun = {
      ...baseRun,
      id: '10000000-0000-0000-0000-000000000099',
      status: 'running' as const,
      completed_at: undefined,
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(createStrategyResponse())
      .mockResolvedValueOnce(createRunsResponse([baseRun, runningRun], 2));
    vi.stubGlobal('fetch', fetchMock);

    render(<RunsPage />, { wrapper: Wrapper });

    expect(await screen.findByTestId('runs-table')).toBeInTheDocument();
    expect(screen.getByText('Duration')).toBeInTheDocument();
    expect(screen.getByText('1m 0s')).toBeInTheDocument();
    expect(screen.getByText('Running…')).toBeInTheDocument();
  });
});
