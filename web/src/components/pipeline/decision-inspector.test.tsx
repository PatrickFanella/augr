import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { DecisionInspector } from '@/components/pipeline/decision-inspector'

vi.mock('@/components/ui/dialog', () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children, ...props }: React.HTMLAttributes<HTMLDivElement>) => <div {...props}>{children}</div>,
}))

describe('DecisionInspector', () => {
  it('renders structured output safely', () => {
    render(
      <DecisionInspector
        decision={{
          id: 'dec-1',
          pipeline_run_id: 'run-1',
          agent_role: 'trader',
          phase: 'trading',
          output_text: 'Buy',
          output_structured: { action: 'buy', confidence: 0.82 },
          created_at: '2026-04-22T00:00:00Z',
        }}
        onClose={() => {}}
      />,
    )

    expect(screen.getByTestId('inspector-structured')).toHaveTextContent('confidence')
  })
})
