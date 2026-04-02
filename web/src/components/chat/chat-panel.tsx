import { useEffect, useRef } from 'react'

import { Badge } from '@/components/ui/badge'

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  agent_role?: string
  created_at: string
}

interface ChatPanelProps {
  messages: ChatMessage[]
}

export function ChatPanel({ messages }: ChatPanelProps) {
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages.length])

  return (
    <div
      className="flex flex-1 flex-col gap-3 overflow-y-auto p-4"
      data-testid="chat-panel"
    >
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
  )
}
