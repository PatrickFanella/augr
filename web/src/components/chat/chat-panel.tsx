import { type ReactNode, useEffect, useRef, useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  agent_role?: string
  created_at: string
}

interface ChatPanelProps {
  messages: ChatMessage[]
  onSendMessage?: (content: string) => void
  isLoading?: boolean
  header?: ReactNode
}

export function ChatPanel({ messages, onSendMessage, isLoading = false, header }: ChatPanelProps) {
  const [inputValue, setInputValue] = useState('')
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (typeof bottomRef.current?.scrollIntoView === 'function') {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [messages, isLoading])

  function handleSend() {
    const trimmed = inputValue.trim()
    if (!trimmed || isLoading) {
      return
    }

    onSendMessage?.(trimmed)
    setInputValue('')
  }

  return (
    <div className="flex h-full flex-1 flex-col" data-testid="chat-panel">
      {header ? (
        <div className="border-b px-4 py-3" data-testid="chat-panel-header">
          {header}
        </div>
      ) : null}

      <div className="flex flex-1 flex-col gap-3 overflow-y-auto p-4">
        {messages.length === 0 ? (
          <p className="text-center text-sm text-muted-foreground">
            No messages yet.
          </p>
        ) : (
          messages.map((msg) => (
            <div
              key={msg.id}
              className={`flex ${
                msg.role === 'user' ? 'justify-end' : 'justify-start'
              }`}
            >
              <div
                className={`max-w-[80%] rounded-lg px-3 py-2 text-sm ${
                  msg.role === 'user'
                    ? 'bg-primary text-primary-foreground'
                    : 'bg-muted'
                }`}
              >
                {msg.role === 'assistant' && msg.agent_role ? (
                  <Badge variant="outline" className="mb-1 text-xs">
                    {msg.agent_role}
                  </Badge>
                ) : null}
                <p className="whitespace-pre-wrap">{msg.content}</p>
                <time className="mt-1 block text-xs opacity-60">
                  {new Date(msg.created_at).toLocaleTimeString()}
                </time>
              </div>
            </div>
          ))
        )}
        <div ref={bottomRef} />
      </div>

      {isLoading ? (
        <div className="flex items-center gap-1 px-4 py-2 text-xs text-muted-foreground" data-testid="typing-indicator">
          <span className="animate-pulse">●</span>
          <span className="animate-pulse" style={{ animationDelay: '0.2s' }}>●</span>
          <span className="animate-pulse" style={{ animationDelay: '0.4s' }}>●</span>
        </div>
      ) : null}

      {onSendMessage ? (
        <div className="flex gap-2 border-t p-3" data-testid="chat-input-bar">
          <textarea
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                handleSend()
              }
            }}
            placeholder="Type a message..."
            disabled={isLoading}
            rows={1}
            className="flex-1 resize-none rounded-md border border-input bg-transparent px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"
            data-testid="chat-input"
          />
          <Button
            onClick={handleSend}
            disabled={isLoading || !inputValue.trim()}
            size="sm"
            data-testid="chat-send-button"
          >
            {isLoading ? 'Sending…' : 'Send'}
          </Button>
        </div>
      ) : null}
    </div>
  )
}
