import { useMemo } from 'react'

import { Badge } from '@/components/ui/badge'
import type { OptionSnapshot } from '@/lib/api/types'
import { cn } from '@/lib/utils'

interface ChainTableProps {
  data: OptionSnapshot[]
  currentPrice?: number
}

function formatPct(value: number) {
  return `${(value * 100).toFixed(1)}%`
}

function formatNum(value: number, decimals = 2) {
  return value.toFixed(decimals)
}

function formatNumber(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(value)
}

export function ChainTable({ data, currentPrice }: ChainTableProps) {
  const grouped = useMemo(() => {
    const map = new Map<string, OptionSnapshot[]>()
    for (const snap of data) {
      const key = snap.contract.expiry
      const list = map.get(key)
      if (list) {
        list.push(snap)
      } else {
        map.set(key, [snap])
      }
    }
    // Sort groups by expiry ascending
    const sorted = [...map.entries()].sort(([a], [b]) => a.localeCompare(b))
    // Sort snapshots within each group by strike ascending
    for (const [, snapshots] of sorted) {
      snapshots.sort((a, b) => a.contract.strike - b.contract.strike)
    }
    return sorted
  }, [data])

  if (!data.length) {
    return (
      <p className="py-6 text-center text-sm text-muted-foreground">
        No options data available.
      </p>
    )
  }

  return (
    <div className="space-y-4">
      {grouped.map(([expiry, snapshots]) => (
        <div key={expiry}>
          <div className="mb-2 border-b border-border pb-1">
            <span className="font-mono text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">
              Expiry {expiry}
            </span>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left font-mono text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
                  <th className="pb-2 pr-3 font-medium">Type</th>
                  <th className="pb-2 pr-3 text-right font-medium">Strike</th>
                  <th className="pb-2 pr-3 text-right font-medium">Bid</th>
                  <th className="pb-2 pr-3 text-right font-medium">Ask</th>
                  <th className="pb-2 pr-3 text-right font-medium">Mid</th>
                  <th className="pb-2 pr-3 text-right font-medium">Delta</th>
                  <th className="pb-2 pr-3 text-right font-medium">IV</th>
                  <th className="pb-2 pr-3 text-right font-medium">Vol</th>
                  <th className="pb-2 text-right font-medium">OI</th>
                </tr>
              </thead>
              <tbody>
                {snapshots.map((snap) => {
                  const isItm = currentPrice
                    ? snap.contract.option_type === 'call'
                      ? snap.contract.strike < currentPrice
                      : snap.contract.strike > currentPrice
                    : false
                  const isAtm =
                    currentPrice != null &&
                    Math.abs(snap.contract.strike - currentPrice) / currentPrice < 0.005

                  return (
                    <tr
                      key={snap.contract.occ_symbol}
                      className={cn(
                        'border-b border-border last:border-0',
                        isAtm
                          ? 'bg-primary/10'
                          : isItm
                            ? 'bg-sky-500/8'
                            : '',
                      )}
                    >
                      <td className="py-2 pr-3">
                        <Badge
                          variant={snap.contract.option_type === 'call' ? 'success' : 'destructive'}
                        >
                          {snap.contract.option_type}
                        </Badge>
                      </td>
                      <td className="py-2 pr-3 text-right font-mono text-[13px]">
                        {formatNum(snap.contract.strike)}
                      </td>
                      <td className="py-2 pr-3 text-right font-mono text-[13px] text-muted-foreground">
                        {formatNum(snap.bid)}
                      </td>
                      <td className="py-2 pr-3 text-right font-mono text-[13px] text-muted-foreground">
                        {formatNum(snap.ask)}
                      </td>
                      <td className="py-2 pr-3 text-right font-mono text-[13px] font-medium">
                        {formatNum(snap.mid)}
                      </td>
                      <td
                        className={cn(
                          'py-2 pr-3 text-right font-mono text-[13px]',
                          snap.greeks.delta >= 0 ? 'text-green-500' : 'text-red-500',
                        )}
                      >
                        {formatNum(snap.greeks.delta, 3)}
                      </td>
                      <td className="py-2 pr-3 text-right font-mono text-[13px]">
                        {formatPct(snap.greeks.iv)}
                      </td>
                      <td className="py-2 pr-3 text-right font-mono text-[13px] text-muted-foreground">
                        {formatNumber(snap.volume)}
                      </td>
                      <td className="py-2 text-right font-mono text-[13px] text-muted-foreground">
                        {formatNumber(snap.open_interest)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      ))}
    </div>
  )
}
