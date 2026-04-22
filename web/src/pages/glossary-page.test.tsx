import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'

import { GlossaryPage, GLOSSARY_TERMS } from '@/pages/glossary-page'

function renderPage() {
  return render(
    <MemoryRouter>
      <GlossaryPage />
    </MemoryRouter>,
  )
}

describe('GlossaryPage', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders the page heading', () => {
    renderPage()
    expect(screen.getByTestId('glossary-page')).toBeInTheDocument()
  })

  it('renders all terms by default', () => {
    renderPage()
    for (const term of GLOSSARY_TERMS) {
      expect(screen.getByTestId(`glossary-term-${term.slug}`)).toBeInTheDocument()
    }
  })

  it('filters terms by search query (term name)', () => {
    renderPage()
    const input = screen.getByTestId('glossary-search')
    fireEvent.change(input, { target: { value: 'kill switch' } })

    expect(screen.getByTestId('glossary-term-kill-switch')).toBeInTheDocument()
    expect(screen.queryByTestId('glossary-term-backtest')).not.toBeInTheDocument()
  })

  it('filters terms by definition keyword', () => {
    renderPage()
    const input = screen.getByTestId('glossary-search')
    fireEvent.change(input, { target: { value: 'websocket' } })

    expect(screen.getByTestId('glossary-term-realtime')).toBeInTheDocument()
  })

  it('shows empty state when no terms match', () => {
    renderPage()
    const input = screen.getByTestId('glossary-search')
    fireEvent.change(input, { target: { value: 'zzznomatchzzz' } })

    expect(screen.getByTestId('glossary-empty')).toBeInTheDocument()
  })

  it('groups terms alphabetically', () => {
    renderPage()
    const headings = screen.getAllByRole('heading', { level: 2 })
    const letters = headings.map((h) => h.textContent)
    const sorted = [...letters].sort()
    expect(letters).toEqual(sorted)
  })
})
