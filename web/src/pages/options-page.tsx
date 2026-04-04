import { useQuery } from '@tanstack/react-query'
import { Search, TrendingUp } from 'lucide-react'
import { useState } from 'react'

import { PageHeader } from '@/components/layout/page-header'
import { ChainTable } from '@/components/options/chain-table'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'

type OptionTypeFilter = '' | 'call' | 'put'

export function OptionsPage() {
  const [draftTicker, setDraftTicker] = useState('')
  const [draftExpiry, setDraftExpiry] = useState('')
  const [draftType, setDraftType] = useState<OptionTypeFilter>('')

  const [ticker, setTicker] = useState('')
  const [expiry, setExpiry] = useState('')
  const [optionType, setOptionType] = useState<OptionTypeFilter>('')

  const { data, isLoading, isError, isFetched, refetch } = useQuery({
    queryKey: ['options-chain', ticker, expiry, optionType],
    queryFn: () =>
      apiClient.getOptionsChain(ticker, {
        expiry: expiry || undefined,
        type: optionType || undefined,
      }),
    enabled: false,
  })

  function loadChain() {
    const normalized = draftTicker.trim().toUpperCase()
    if (!normalized) return
    setTicker(normalized)
    setExpiry(draftExpiry)
    setOptionType(draftType)
    // refetch after state settles
    setTimeout(() => void refetch(), 0)
  }

  return (
    <div className="space-y-4" data-testid="options-page">
      <PageHeader
        title="Options chain"
        description="Look up option chains by underlying ticker."
      />

      <Card>
        <CardHeader>
          <CardTitle>Lookup</CardTitle>
          <CardDescription>Enter a ticker and optional filters, then load the chain.</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            className="grid gap-3 lg:grid-cols-[200px_170px_160px_auto]"
            onSubmit={(e) => {
              e.preventDefault()
              loadChain()
            }}
          >
            <Input
              placeholder="Ticker (e.g. AAPL)"
              value={draftTicker}
              onChange={(e) => setDraftTicker(e.target.value)}
              aria-label="Underlying ticker"
            />
            <Input
              type="date"
              value={draftExpiry}
              onChange={(e) => setDraftExpiry(e.target.value)}
              aria-label="Expiry date filter"
            />
            <select
              value={draftType}
              onChange={(e) => setDraftType(e.target.value as OptionTypeFilter)}
              aria-label="Option type"
              className="flex h-9 w-full rounded-md border border-input bg-card px-3 py-1 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            >
              <option value="">All types</option>
              <option value="call">Calls</option>
              <option value="put">Puts</option>
            </select>
            <Button type="submit" disabled={!draftTicker.trim()}>
              <Search className="size-4" />
              Load Chain
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>
            {ticker ? `${ticker} options` : 'Options data'}
          </CardTitle>
          <CardDescription>
            {isLoading
              ? 'Loading chain...'
              : isError
                ? 'Failed to load options chain'
                : !isFetched
                  ? 'Enter a ticker above to get started'
                  : `${data?.length ?? 0} contracts loaded`}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <div className="space-y-3" data-testid="options-loading">
              {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className="flex items-center gap-3 rounded-lg border p-3">
                  <div className="h-4 w-16 animate-pulse rounded bg-muted" />
                  <div className="h-4 w-20 animate-pulse rounded bg-muted" />
                  <div className="h-4 w-20 animate-pulse rounded bg-muted" />
                  <div className="ml-auto h-4 w-14 animate-pulse rounded bg-muted" />
                </div>
              ))}
            </div>
          ) : isError ? (
            <div className="space-y-3" data-testid="options-error">
              <p className="text-sm text-muted-foreground">
                Unable to load options chain. Ensure the API server is running.
              </p>
              <Button type="button" variant="outline" size="sm" onClick={() => void refetch()}>
                Retry
              </Button>
            </div>
          ) : !isFetched ? (
            <div className="flex flex-col items-center gap-2 py-8 text-center" data-testid="options-empty">
              <TrendingUp className="size-8 text-muted-foreground" />
              <p className="text-sm text-muted-foreground">
                No data yet. Search for a ticker to view its options chain.
              </p>
            </div>
          ) : (
            <ChainTable data={data ?? []} />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
