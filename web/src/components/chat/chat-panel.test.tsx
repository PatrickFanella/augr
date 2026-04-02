import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'

import { ChatPanel, type ChatMessage } from './chat-panel'

beforeAll(() => {
  Element.prototype.scrollIntoView = () => {}
})

afterEach(cleanup)

const userMsg: ChatMessage = {
  id: '1',
  role: 'user',
  content: 'Why did you buy?',
  created_at: new Date().toISOString(),
}

const assistantMsg: ChatMessage = {
  id: '2',
  role: 'assistant',
  content: 'The bull case outweighed bear signals.',
  agent_role: 'trader',
  created_at: new Date().toISOString(),
}

describe('ChatPanel', () => {
  it('renders empty state', () => {
    render(<ChatPanel messages={[]} />)
    expect(screen.getByText('No messages yet.')).toBeInTheDocument()
  })

  it('renders user message right-aligned with primary background', () => {
    const { container } = render(<ChatPanel messages={[userMsg]} />)
    const msgWrapper = container.querySelector('[class*="justify-end"]')
    expect(msgWrapper).toBeTruthy()
    expect(screen.getByText('Why did you buy?')).toBeInTheDocument()
  })

  it('renders assistant message left-aligned with muted background', () => {
    const { container } = render(<ChatPanel messages={[assistantMsg]} />)
    const msgWrapper = container.querySelector('[class*="justify-start"]')
    expect(msgWrapper).toBeTruthy()
    expect(screen.getByText('The bull case outweighed bear signals.')).toBeInTheDocument()
  })

  it('shows agent role badge on assistant messages', () => {
    render(<ChatPanel messages={[assistantMsg]} />)
    expect(screen.getByText('trader')).toBeInTheDocument()
  })

  it('does not show agent role badge on user messages', () => {
    render(<ChatPanel messages={[userMsg]} />)
    expect(screen.queryByText('trader')).not.toBeInTheDocument()
  })

  it('renders multiple messages in order', () => {
    render(<ChatPanel messages={[userMsg, assistantMsg]} />)
    const panel = screen.getByTestId('chat-panel')
    const texts = Array.from(panel.querySelectorAll('p.whitespace-pre-wrap')).map(el => el.textContent)
    expect(texts).toEqual(['Why did you buy?', 'The bull case outweighed bear signals.'])
  })
})
