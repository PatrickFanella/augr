import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { RealtimePage } from '@/pages/realtime-page'

describe('RealtimePage', () => {
  afterEach(() => {
    cleanup()
  })

  it('renders both panels with expected content', () => {
    render(<RealtimePage />)

    expect(screen.getByTestId('realtime-page')).toBeInTheDocument()
    expect(screen.getByText('Event Feed')).toBeInTheDocument()
    expect(screen.getByText('Select an event to start a conversation')).toBeInTheDocument()
  })

  it('renders two panel containers', () => {
    render(<RealtimePage />)

    const wrapper = screen.getByTestId('realtime-page')
    const panels = wrapper.querySelectorAll(':scope > div')
    expect(panels).toHaveLength(2)
  })
})
