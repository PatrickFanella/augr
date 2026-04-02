import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import { ChatPanel, type ChatMessage } from './chat-panel'

beforeAll(() => {
  Element.prototype.scrollIntoView = () => {}
})

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
  afterEach(() => {
    cleanup()
  })

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
    const texts = Array.from(panel.querySelectorAll('p.whitespace-pre-wrap')).map((el) => el.textContent)
    expect(texts).toEqual(['Why did you buy?', 'The bull case outweighed bear signals.'])
  })

  it('renders input bar when onSendMessage provided', () => {
    render(<ChatPanel messages={[]} onSendMessage={vi.fn()} />)
    expect(screen.getByTestId('chat-input-bar')).toBeInTheDocument()
  })

  it('does not render input bar when onSendMessage not provided', () => {
    render(<ChatPanel messages={[]} />)
    expect(screen.queryByTestId('chat-input-bar')).not.toBeInTheDocument()
  })

  it('calls onSendMessage on button click', () => {
    const onSendMessage = vi.fn()
    render(<ChatPanel messages={[]} onSendMessage={onSendMessage} />)

    fireEvent.change(screen.getByTestId('chat-input'), { target: { value: 'hello world' } })
    fireEvent.click(screen.getByTestId('chat-send-button'))

    expect(onSendMessage).toHaveBeenCalledWith('hello world')
    expect(screen.getByTestId('chat-input')).toHaveValue('')
  })

  it('calls onSendMessage on Enter key', () => {
    const onSendMessage = vi.fn()
    render(<ChatPanel messages={[]} onSendMessage={onSendMessage} />)

    fireEvent.change(screen.getByTestId('chat-input'), { target: { value: 'enter send' } })
    fireEvent.keyDown(screen.getByTestId('chat-input'), { key: 'Enter' })

    expect(onSendMessage).toHaveBeenCalledWith('enter send')
  })

  it('does not send on Shift+Enter', () => {
    const onSendMessage = vi.fn()
    render(<ChatPanel messages={[]} onSendMessage={onSendMessage} />)

    fireEvent.change(screen.getByTestId('chat-input'), { target: { value: 'multi line' } })
    fireEvent.keyDown(screen.getByTestId('chat-input'), { key: 'Enter', shiftKey: true })

    expect(onSendMessage).not.toHaveBeenCalled()
  })

  it('disables input and button when isLoading', () => {
    render(<ChatPanel messages={[]} onSendMessage={vi.fn()} isLoading />)

    expect(screen.getByTestId('chat-input')).toBeDisabled()
    expect(screen.getByTestId('chat-send-button')).toBeDisabled()
  })

  it('shows typing indicator when isLoading', () => {
    render(<ChatPanel messages={[]} onSendMessage={vi.fn()} isLoading />)
    expect(screen.getByTestId('typing-indicator')).toBeInTheDocument()
  })
})
