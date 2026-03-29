import { useQuery } from '@tanstack/react-query'
import { ArrowUpRight, PlugZap, RadioTower, Workflow } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { apiClient } from '@/lib/api/client'
import { getApiBaseUrl, getWebSocketUrl } from '@/lib/config'

const checklistItems = [
  {
    title: 'Typed API client',
    description: 'REST helpers now mirror the current Go routes, list envelopes, and JSON field names.',
    icon: Workflow,
  },
  {
    title: 'Realtime hook',
    description: 'A WebSocket hook supports subscribe, unsubscribe, and reconnect flows for backend events.',
    icon: RadioTower,
  },
  {
    title: 'UI foundation',
    description: 'Tailwind CSS 4 and shadcn-compatible component primitives are ready for page-specific work.',
    icon: PlugZap,
  },
]

export function DashboardPage() {
  const apiBaseUrl = getApiBaseUrl()
  const websocketUrl = getWebSocketUrl()
  const healthQuery = useQuery({
    queryKey: ['health'],
    queryFn: () => apiClient.health(),
    retry: false,
  })

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]">
      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>Starter workspace</CardTitle>
            <CardDescription>
              The frontend shell is wired for navigation now, with individual feature pages intentionally left for future issues.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 md:grid-cols-3">
            {checklistItems.map(({ title, description, icon: Icon }) => (
              <div key={title} className="rounded-xl border bg-secondary/40 p-4">
                <div className="mb-3 flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
                  <Icon className="size-5" />
                </div>
                <h3 className="font-medium">{title}</h3>
                <p className="mt-2 text-sm text-muted-foreground">{description}</p>
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Next implementation slices</CardTitle>
            <CardDescription>
              These placeholders keep the application structure visible without prematurely expanding the scope of this issue.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ul className="space-y-3 text-sm text-muted-foreground">
              <li>• Strategies can attach list, detail, and run actions to the shared API client.</li>
              <li>• Runs can combine polling with realtime subscriptions via strategy and run identifiers.</li>
              <li>• Portfolio and risk pages can reuse the common layout, cards, and query defaults.</li>
            </ul>
          </CardContent>
        </Card>
      </div>

      <div className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>Backend wiring</CardTitle>
            <CardDescription>Environment-aware defaults point the frontend at the local API server.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4 text-sm">
            <div className="rounded-lg border bg-secondary/40 p-3">
              <p className="font-medium">API base URL</p>
              <p className="mt-1 break-all text-muted-foreground">{apiBaseUrl}</p>
            </div>
            <div className="rounded-lg border bg-secondary/40 p-3">
              <p className="font-medium">WebSocket URL</p>
              <p className="mt-1 break-all text-muted-foreground">{websocketUrl}</p>
            </div>
            <Button asChild className="w-full">
              <a href={`${apiBaseUrl}/health`} rel="noreferrer" target="_blank">
                Open health endpoint
                <ArrowUpRight className="size-4" />
              </a>
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Connection check</CardTitle>
            <CardDescription>
              TanStack Query is active; this card exercises the shared API client against the public health endpoint.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {healthQuery.isLoading ? (
              <p className="text-sm text-muted-foreground">Checking backend health…</p>
            ) : healthQuery.isError ? (
              <div className="rounded-lg border border-dashed p-3 text-sm text-muted-foreground">
                Backend health is currently unavailable. Start the API server to see a live status here.
              </div>
            ) : (
              <div className="rounded-lg border bg-secondary/40 p-3 text-sm">
                <p className="font-medium">Backend status</p>
                <p className="mt-1 text-muted-foreground">{healthQuery.data?.status ?? 'unknown'}</p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
