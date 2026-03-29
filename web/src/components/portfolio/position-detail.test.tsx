import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PositionDetail } from '@/components/portfolio/position-detail'
import type { Position } from '@/lib/api/types'

const mockPosition: Position = {
  id: 'pos-1',
  strategy_id: 'strat-1',
  ticker: 'GOOG',
  side: 'long',
  quantity: 15,
  avg_entry: 175.0,
  current_price: 180.0,
  unrealized_pnl: 75.0,
  realized_pnl: 120.5,
  stop_loss: 170.0,
  take_profit: 200.0,
  opened_at: '2025-01-20T09:30:00Z',
}

describe('PositionDetail', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders position details', () => {
    render(<PositionDetail position={mockPosition} onClose={vi.fn()} />)

    expect(screen.getByTestId('position-detail')).toBeInTheDocument()
    expect(screen.getByText('GOOG')).toBeInTheDocument()
    expect(screen.getByText('long')).toBeInTheDocument()
    expect(screen.getByText('$175.00')).toBeInTheDocument()
    expect(screen.getByText('$180.00')).toBeInTheDocument()
    expect(screen.getByText('15')).toBeInTheDocument()
    expect(screen.getByText('$75.00')).toBeInTheDocument()
    expect(screen.getByText('$120.50')).toBeInTheDocument()
    expect(screen.getByText('$170.00')).toBeInTheDocument()
    expect(screen.getByText('$200.00')).toBeInTheDocument()
    expect(screen.getByText('strat-1')).toBeInTheDocument()
  })

  it('calls onClose when close button is clicked', async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()

    render(<PositionDetail position={mockPosition} onClose={onClose} />)

    const closeButton = screen.getByRole('button', { name: 'Close position details' })
    await user.click(closeButton)

    expect(onClose).toHaveBeenCalledOnce()
  })

  it('shows dashes for optional fields when not present', () => {
    const minimalPosition: Position = {
      id: 'pos-2',
      ticker: 'BTC',
      side: 'short',
      quantity: 1,
      avg_entry: 50000.0,
      realized_pnl: 0,
      opened_at: '2025-02-01T12:00:00Z',
    }

    render(<PositionDetail position={minimalPosition} onClose={vi.fn()} />)

    expect(screen.getByText('BTC')).toBeInTheDocument()
    expect(screen.getByText('short')).toBeInTheDocument()
    const dashes = screen.getAllByText('—')
    expect(dashes.length).toBeGreaterThanOrEqual(3)
  })
})
