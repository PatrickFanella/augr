import { type FormEvent, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import type { MarketType, StrategyCreateRequest } from '@/lib/api/types'

interface CreateStrategyDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (data: StrategyCreateRequest) => void
  isSubmitting?: boolean
}

const marketTypes: MarketType[] = ['stock', 'crypto', 'polymarket']

export function CreateStrategyDialog({
  open,
  onOpenChange,
  onSubmit,
  isSubmitting,
}: CreateStrategyDialogProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [ticker, setTicker] = useState('')
  const [marketType, setMarketType] = useState<MarketType>('stock')
  const [scheduleCron, setScheduleCron] = useState('')
  const [isPaper, setIsPaper] = useState(true)
  const [isActive, setIsActive] = useState(false)
  const [configJson, setConfigJson] = useState('{}')
  const [configError, setConfigError] = useState<string | null>(null)

  function resetForm() {
    setName('')
    setDescription('')
    setTicker('')
    setMarketType('stock')
    setScheduleCron('')
    setIsPaper(true)
    setIsActive(false)
    setConfigJson('{}')
    setConfigError(null)
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) {
      resetForm()
    }
    onOpenChange(nextOpen)
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()

    let config: unknown = {}
    try {
      config = JSON.parse(configJson)
      setConfigError(null)
    } catch {
      setConfigError('Invalid JSON')
      return
    }

    onSubmit({
      name,
      description: description || undefined,
      ticker: ticker.toUpperCase(),
      market_type: marketType,
      schedule_cron: scheduleCron || undefined,
      config,
      status: isActive ? 'active' : 'inactive',
      is_paper: isPaper,
    })
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent data-testid="create-strategy-dialog">
        <DialogHeader>
          <DialogTitle>Create strategy</DialogTitle>
          <DialogDescription>
            Configure a new trading strategy. Required fields are marked with *.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="strategy-name">Name *</Label>
              <Input
                id="strategy-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="AAPL Momentum"
                required
                data-testid="strategy-name-input"
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="strategy-ticker">Ticker *</Label>
              <Input
                id="strategy-ticker"
                value={ticker}
                onChange={(e) => setTicker(e.target.value)}
                placeholder="AAPL"
                required
                data-testid="strategy-ticker-input"
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="strategy-description">Description</Label>
            <Input
              id="strategy-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description"
            />
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="strategy-market-type">Market type *</Label>
              <select
                id="strategy-market-type"
                value={marketType}
                onChange={(e) => setMarketType(e.target.value as MarketType)}
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                data-testid="strategy-market-type-select"
              >
                {marketTypes.map((mt) => (
                  <option key={mt} value={mt}>
                    {mt}
                  </option>
                ))}
              </select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="strategy-schedule">Schedule (cron)</Label>
              <Input
                id="strategy-schedule"
                value={scheduleCron}
                onChange={(e) => setScheduleCron(e.target.value)}
                placeholder="0 9 * * 1-5"
              />
            </div>
          </div>

          <div className="flex gap-6">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={isPaper}
                onChange={(e) => setIsPaper(e.target.checked)}
                className="rounded border-input"
                data-testid="strategy-paper-checkbox"
              />
              Paper trading
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={isActive}
                onChange={(e) => setIsActive(e.target.checked)}
                className="rounded border-input"
                data-testid="strategy-active-checkbox"
              />
              Active
            </label>
          </div>

          <div className="space-y-2">
            <Label htmlFor="strategy-config">Configuration (JSON)</Label>
            <Textarea
              id="strategy-config"
              value={configJson}
              onChange={(e) => {
                setConfigJson(e.target.value)
                setConfigError(null)
              }}
              rows={4}
              className="font-mono text-xs"
              data-testid="strategy-config-textarea"
            />
            {configError ? (
              <p className="text-xs text-destructive" data-testid="config-error">
                {configError}
              </p>
            ) : null}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={isSubmitting || !name || !ticker}>
              {isSubmitting ? 'Creating…' : 'Create strategy'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
