import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { OptionsPage } from '@/pages/options-page'

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    getOptionsChain: vi.fn().mockResolvedValue({ calls: [], puts: [] }),
  },
}))

function renderAt(search = '') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[`/options${search}`]}>
        <Routes>
          <Route path="/options" element={<OptionsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('OptionsPage', () => {
  afterEach(() => {
    cleanup()
    vi.clearAllMocks()
  })

  it('renders the page without crashing', () => {
    renderAt()
    expect(screen.getByTestId('options-page')).toBeInTheDocument()
  })

  it('seeds ticker input from ?ticker= URL param', async () => {
    renderAt('?ticker=AAPL')

    await waitFor(() => {
      const input = screen.getByRole<HTMLInputElement>('textbox', { name: /underlying ticker/i })
      expect(input.value).toBe('AAPL')
    })
  })

  it('calls getOptionsChain when ?ticker= param is present on mount', async () => {
    const { apiClient } = await import('@/lib/api/client')
    renderAt('?ticker=TSLA')

    await waitFor(() => {
      expect(apiClient.getOptionsChain).toHaveBeenCalledWith('TSLA', expect.any(Object))
    })
  })
})
