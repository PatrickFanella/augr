import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { MemoriesPage } from '@/pages/memories-page'

function Wrapper({ children }: { children: React.ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const baseMemory = {
  id: '00000000-0000-0000-0000-000000000001',
  agent_role: 'trader' as const,
  situation: 'Momentum was weakening after a large opening gap and low follow-through.',
  recommendation: 'Wait for confirmation before sizing into a breakout.',
  outcome: 'Avoided a failed breakout and preserved buying power.',
  relevance_score: 0.87,
  created_at: '2025-01-01T12:00:00Z',
}

describe('MemoriesPage', () => {
  it('renders the memory list and detail view on successful fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [baseMemory], limit: 10, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<MemoriesPage />, { wrapper: Wrapper })

    expect(await screen.findByText(/Momentum was weakening/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /view details/i }))

    const detailDialog = await screen.findByTestId('memory-detail-dialog')
    expect(detailDialog).toBeInTheDocument()
    expect(detailDialog).toHaveTextContent(baseMemory.recommendation)
  })

  it('submits search text and role filters through the list endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [baseMemory], limit: 10, offset: 0 }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<MemoriesPage />, { wrapper: Wrapper })

    await screen.findByTestId('memories-page')

    fireEvent.change(screen.getByLabelText(/search memories/i), {
      target: { value: 'breakout risk' },
    })
    fireEvent.change(screen.getByLabelText(/agent role/i), {
      target: { value: 'trader' },
    })
    fireEvent.click(screen.getByTestId('apply-memory-filters'))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))

    const requestUrl = new URL(fetchMock.mock.calls[1][0].toString())
    expect(requestUrl.pathname).toBe('/api/v1/memories')
    expect(requestUrl.searchParams.get('q')).toBe('breakout risk')
    expect(requestUrl.searchParams.get('agent_role')).toBe('trader')
    expect(requestUrl.searchParams.get('limit')).toBe('10')
    expect(requestUrl.searchParams.get('offset')).toBe('0')
  })

  it('supports deleting a memory and moving to the next page', async () => {
    const pageOne = Array.from({ length: 10 }, (_, index) => ({
      ...baseMemory,
      id: `00000000-0000-0000-0000-0000000000${String(index + 1).padStart(2, '0')}`,
      situation: `Situation ${index + 1}`,
      recommendation: `Recommendation ${index + 1}`,
    }))
    const pageTwo = [
      {
        ...baseMemory,
        id: '00000000-0000-0000-0000-000000000099',
        situation: 'Situation 11',
        recommendation: 'Recommendation 11',
      },
    ]

    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: pageOne, limit: 10, offset: 0 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: async () => ({}),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: pageOne, limit: 10, offset: 0 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: pageTwo, limit: 10, offset: 10 }),
      })
    vi.stubGlobal('fetch', fetchMock)

    render(<MemoriesPage />, { wrapper: Wrapper })

    expect(await screen.findByText('Situation 1')).toBeInTheDocument()

    fireEvent.click(screen.getByTestId('delete-memory-00000000-0000-0000-0000-000000000001'))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3))

    const deleteCall = fetchMock.mock.calls[1]
    expect(deleteCall[1]?.method).toBe('DELETE')

    fireEvent.click(screen.getByRole('button', { name: /next/i }))

    expect(await screen.findByText('Situation 11')).toBeInTheDocument()
    const pageTwoUrl = new URL(fetchMock.mock.calls[3][0].toString())
    expect(pageTwoUrl.searchParams.get('offset')).toBe('10')
  })
})
