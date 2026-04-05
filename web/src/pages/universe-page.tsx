import { useMutation, useQuery } from '@tanstack/react-query'
import { Globe, Loader2, RefreshCw, Search } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router-dom'

import { PageHeader } from '@/components/layout/page-header'
import { WatchlistTable } from '@/components/universe/watchlist-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { apiClient } from '@/lib/api/client'
import type { ScoredTicker, TrackedTicker } from '@/lib/api/types'

type Tab = 'browser' | 'watchlist'

export function UniversePage() {
  const [tab, setTab] = useState<Tab>('browser')
  const [search, setSearch] = useState('')
  const [indexGroup, setIndexGroup] = useState('')
  const [page, setPage] = useState(0)
  const pageSize = 50

  // Universe browser query
  const universeQuery = useQuery({
    queryKey: ['universe', search, indexGroup, page],
    queryFn: () =>
      apiClient.listUniverse({
        search: search || undefined,
        index_group: indexGroup || undefined,
        limit: pageSize,
        offset: page * pageSize,
      }),
  })

  // Watchlist query
  const watchlistQuery = useQuery({
    queryKey: ['universe-watchlist'],
    queryFn: () => apiClient.getWatchlist(30),
    enabled: tab === 'watchlist',
  })

  // Refresh mutation
  const refreshMutation = useMutation({
    mutationFn: () => apiClient.refreshUniverse(),
    onSuccess: () => universeQuery.refetch(),
  })

  // Scan mutation
  const scanMutation = useMutation({
    mutationFn: () => apiClient.runPreMarketScan(),
    onSuccess: () => watchlistQuery.refetch(),
  })

  const tickers: TrackedTicker[] = universeQuery.data?.data ?? []
  const watchlist: ScoredTicker[] = watchlistQuery.data ?? []

  return (
    <div className="space-y-4" data-testid="universe-page">
      <PageHeader
        title="Universe"
        description="Tracked tickers and pre-market watchlist."
        meta={<Globe className="size-4 text-muted-foreground" />}
      />

      {/* Tab switcher */}
      <div className="flex gap-1">
        <button
          type="button"
          onClick={() => setTab('browser')}
          className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
            tab === 'browser'
              ? 'bg-primary/14 text-foreground'
              : 'text-muted-foreground hover:bg-accent/70 hover:text-foreground'
          }`}
        >
          Universe Browser
        </button>
        <button
          type="button"
          onClick={() => setTab('watchlist')}
          className={`rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
            tab === 'watchlist'
              ? 'bg-primary/14 text-foreground'
              : 'text-muted-foreground hover:bg-accent/70 hover:text-foreground'
          }`}
        >
          Watchlist
        </button>
      </div>

      {tab === 'browser' && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>Universe Browser</CardTitle>
              <Button
                size="sm"
                variant="outline"
                disabled={refreshMutation.isPending}
                onClick={() => refreshMutation.mutate()}
              >
                {refreshMutation.isPending ? (
                  <Loader2 className="mr-1.5 size-3.5 animate-spin" />
                ) : (
                  <RefreshCw className="mr-1.5 size-3.5" />
                )}
                Refresh Universe
              </Button>
            </div>
            {refreshMutation.isSuccess && refreshMutation.data && (
              <p className="text-xs text-muted-foreground">
                Refreshed {refreshMutation.data.count} tickers
              </p>
            )}
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex gap-2">
              <div className="relative flex-1">
                <Search className="absolute left-2.5 top-2.5 size-3.5 text-muted-foreground" />
                <Input
                  placeholder="Search ticker or name..."
                  value={search}
                  onChange={(e) => {
                    setSearch(e.target.value)
                    setPage(0)
                  }}
                  className="pl-8"
                />
              </div>
              <select
                value={indexGroup}
                onChange={(e) => {
                  setIndexGroup(e.target.value)
                  setPage(0)
                }}
                className="flex h-9 rounded-md border border-input bg-card px-3 py-1 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <option value="">All Exchanges</option>
                <option value="nasdaq">NASDAQ</option>
                <option value="nyse">NYSE</option>
                <option value="other">Other</option>
              </select>
            </div>

            {universeQuery.isLoading && (
              <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                Loading...
              </div>
            )}

            {universeQuery.isError && (
              <p className="py-4 text-sm text-destructive">Failed to load universe.</p>
            )}

            {!universeQuery.isLoading && tickers.length === 0 && (
              <p className="py-4 text-sm text-muted-foreground">
                No tickers found. Try refreshing the universe.
              </p>
            )}

            {tickers.length > 0 && (
              <>
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-sm">
                    <thead>
                      <tr className="border-b border-border text-xs font-medium uppercase tracking-wider text-muted-foreground">
                        <th className="px-2 py-2">Ticker</th>
                        <th className="px-2 py-2">Name</th>
                        <th className="px-2 py-2">Active</th>
                        <th className="px-2 py-2">Exchange</th>
                        <th className="px-2 py-2">Index Group</th>
                        <th className="px-2 py-2 text-right">Watch Score</th>
                        <th className="px-2 py-2">Last Scanned</th>
                      </tr>
                    </thead>
                    <tbody>
                      {tickers.map((t) => (
                        <tr key={t.ticker} className="border-b border-border/50 hover:bg-accent/30">
                          <td className="px-2 py-1.5 font-mono font-medium">
                            <Link to={`/stocks/${t.ticker}`} className="text-primary hover:underline">
                              {t.ticker}
                            </Link>
                          </td>
                          <td className="max-w-48 truncate px-2 py-1.5 text-muted-foreground">
                            {t.name}
                          </td>
                          <td className="px-2 py-1.5">
                            <span className={`inline-block size-2 rounded-full ${t.active ? 'bg-emerald-400' : 'bg-muted-foreground/30'}`} />
                          </td>
                          <td className="px-2 py-1.5 text-muted-foreground">{t.exchange}</td>
                          <td className="px-2 py-1.5">
                            <Badge variant="secondary">{t.index_group}</Badge>
                          </td>
                          <td className="px-2 py-1.5 text-right font-mono">
                            {t.watch_score.toFixed(2)}
                          </td>
                          <td className="px-2 py-1.5 text-xs text-muted-foreground">
                            {t.last_scanned
                              ? new Date(t.last_scanned).toLocaleDateString()
                              : '--'}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>

                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">
                    Page {page + 1}
                  </span>
                  <div className="flex gap-1">
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={page === 0}
                      onClick={() => setPage((p) => Math.max(0, p - 1))}
                    >
                      Prev
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={tickers.length < pageSize}
                      onClick={() => setPage((p) => p + 1)}
                    >
                      Next
                    </Button>
                  </div>
                </div>
              </>
            )}
          </CardContent>
        </Card>
      )}

      {tab === 'watchlist' && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>Top 30 Watchlist</CardTitle>
              <Button
                size="sm"
                variant="outline"
                disabled={scanMutation.isPending}
                onClick={() => scanMutation.mutate()}
              >
                {scanMutation.isPending ? (
                  <Loader2 className="mr-1.5 size-3.5 animate-spin" />
                ) : (
                  <Search className="mr-1.5 size-3.5" />
                )}
                Run Pre-Market Scan
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            {watchlistQuery.isLoading && (
              <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                Loading watchlist...
              </div>
            )}

            {scanMutation.isPending && (
              <div className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                Scanning...
              </div>
            )}

            {scanMutation.isSuccess && scanMutation.data && (
              <WatchlistTable tickers={scanMutation.data} />
            )}

            {!scanMutation.data && watchlist.length > 0 && (
              <WatchlistTable tickers={watchlist as unknown as ScoredTicker[]} />
            )}

            {!scanMutation.data &&
              !watchlistQuery.isLoading &&
              watchlist.length === 0 && (
                <p className="py-4 text-sm text-muted-foreground">
                  No watchlist data yet. Run a pre-market scan to populate.
                </p>
              )}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
