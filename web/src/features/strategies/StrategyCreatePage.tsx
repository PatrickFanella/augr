import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { createStrategy, getSettings, getStrategy } from '@/shared/api/endpoints'
import { isApiClientError } from '@/shared/api/errors'
import { strategyCreateRequestSchema } from '@/shared/api/schemas'
import { ConfirmationDialog } from '@/shared/components/ConfirmationDialog'
import { ErrorState, LoadingState } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { KnownMarketType, Strategy, StrategyCreateRequest } from '@/shared/types/domain'

const defaultConfig = '{\n  "fixture": true,\n  "mode": "paper"\n}'
const marketOptions: KnownMarketType[] = ['stock', 'crypto', 'polymarket', 'kalshi', 'options']

type FieldErrors = Partial<Record<'name' | 'ticker' | 'market_type' | 'schedule_cron' | 'config' | 'submit', string>>

function createErrorMessage(error: unknown) {
  if (!isApiClientError(error)) return { message: 'Create failed after submission. Completion is unknown; check the strategy list before retrying.', unknownCompletion: true }
  switch (error.kind) {
    case 'unauthorized':
      return { message: 'Your session is no longer authorized. Sign in again before creating a strategy.' }
    case 'validation':
    case 'bad_request':
      return { message: error.message || 'The strategy was rejected by server validation.' }
    case 'conflict':
      return { message: 'A strategy with matching identity may already exist. Review the list before retrying.' }
    case 'rate_limited':
      return { message: 'Strategy creation is temporarily rate limited. Wait before retrying.' }
    case 'network':
      return { message: 'Network failed while creating the strategy. Completion is unknown; check the strategy list before retrying.', unknownCompletion: true }
    case 'server':
      return { message: 'The server could not safely create the strategy. Check the list before retrying.' }
    default:
      return { message: 'Strategy create did not complete. Check the list before retrying.' }
  }
}

function validateForm(form: HTMLFormElement): { request?: StrategyCreateRequest; errors: FieldErrors } {
  const data = new FormData(form)
  const errors: FieldErrors = {}
  let config: unknown
  try {
    config = JSON.parse(String(data.get('config') ?? '{}'))
  } catch {
    errors.config = 'Config must be valid JSON.'
  }
  const raw = {
    name: String(data.get('name') ?? '').trim(),
    description: String(data.get('description') ?? '').trim() || undefined,
    ticker: String(data.get('ticker') ?? '').trim().toUpperCase(),
    market_type: String(data.get('market_type') ?? 'stock'),
    schedule_cron: String(data.get('schedule_cron') ?? '').trim() || undefined,
    config: config ?? {},
    is_paper: true,
  }
  const parsed = strategyCreateRequestSchema.safeParse(raw)
  if (!parsed.success) {
    for (const issue of parsed.error.issues) {
      const key = issue.path[0]
      if (typeof key === 'string' && key in raw) errors[key as keyof FieldErrors] = issue.message
    }
  }
  return Object.keys(errors).length > 0 ? { errors } : { request: parsed.success ? parsed.data as StrategyCreateRequest : undefined, errors }
}

export function StrategyCreatePage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const settings = useQuery({ queryKey: queryKeys.settings, queryFn: ({ signal }) => getSettings(signal) })
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({})
  const [pendingRequest, setPendingRequest] = useState<StrategyCreateRequest | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogError, setDialogError] = useState<{ message: string; unknownCompletion?: boolean } | null>(null)
  const [formSnapshot, setFormSnapshot] = useState({ name: '', description: '', ticker: '', market_type: 'stock', schedule_cron: '', config: defaultConfig })

  const environmentLabel = useMemo(() => settings.data?.system.environment ?? 'Environment unknown', [settings.data?.system.environment])

  const mutation = useMutation({
    mutationFn: async (request: StrategyCreateRequest) => createStrategy(request),
    retry: false,
    onSuccess: async (created: Strategy) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.strategyList })
      try {
        await queryClient.fetchQuery({ queryKey: queryKeys.strategyDetail(created.id), queryFn: ({ signal }) => getStrategy(created.id, signal), retry: false })
        setDialogOpen(false)
        navigate(`/strategies/${created.id}`)
      } catch {
        setDialogError({ message: 'Strategy was created, but verification fetch failed. Open the strategies list before retrying.', unknownCompletion: true })
      }
    },
    onError: (error) => setDialogError(createErrorMessage(error)),
  })

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = event.currentTarget
    const result = validateForm(form)
    setFieldErrors(result.errors)
    setDialogError(null)
    if (!result.request) return
    setPendingRequest(result.request)
    setDialogOpen(true)
  }

  function updateSnapshot(name: keyof typeof formSnapshot, value: string) {
    setFormSnapshot((current) => ({ ...current, [name]: name === 'ticker' ? value.toUpperCase() : value }))
  }

  return (
    <div className="detail-stack">
      <nav className="breadcrumbs" aria-label="Breadcrumbs">
        <Link to="/cockpit">Cockpit</Link><span aria-hidden="true">/</span><Link to="/strategies">Strategies</Link><span aria-hidden="true">/</span><span>New paper strategy</span>
      </nav>
      <section className="panel hero-panel">
        <p className="eyebrow">Paper strategy create</p>
        <div className="panel-header">
          <div>
            <h1>New paper strategy</h1>
            <p className="muted">Create is paper-only. Live strategy creation is blocked by the server contract.</p>
          </div>
          <span className="status-pill paper">PAPER</span>
        </div>
        {settings.isLoading ? <LoadingState label="Loading environment context…" /> : null}
        {settings.error ? <ErrorState error={settings.error} onRetry={() => void settings.refetch()} /> : null}
        <p className="warning-box" role="status">Environment: {environmentLabel}. Review config carefully; do not paste provider secrets or live credentials.</p>

        <form className="strategy-form" onSubmit={onSubmit} noValidate>
          <label>
            Name
            <input name="name" value={formSnapshot.name} onChange={(event) => updateSnapshot('name', event.target.value)} aria-invalid={Boolean(fieldErrors.name)} />
            {fieldErrors.name ? <span className="error-text">{fieldErrors.name}</span> : null}
          </label>
          <label>
            Ticker
            <input name="ticker" value={formSnapshot.ticker} onChange={(event) => updateSnapshot('ticker', event.target.value)} aria-invalid={Boolean(fieldErrors.ticker)} />
            {fieldErrors.ticker ? <span className="error-text">{fieldErrors.ticker}</span> : null}
          </label>
          <label>
            Market type
            <select name="market_type" value={formSnapshot.market_type} onChange={(event) => updateSnapshot('market_type', event.target.value)}>
              {marketOptions.map((market) => <option key={market} value={market}>{market}</option>)}
            </select>
          </label>
          <label>
            Schedule cron <span className="muted">optional</span>
            <input name="schedule_cron" value={formSnapshot.schedule_cron} onChange={(event) => updateSnapshot('schedule_cron', event.target.value)} placeholder="0 9 * * 1-5" />
            {fieldErrors.schedule_cron ? <span className="error-text">{fieldErrors.schedule_cron}</span> : null}
          </label>
          <label className="form-wide">
            Description <span className="muted">optional</span>
            <textarea name="description" value={formSnapshot.description} onChange={(event) => updateSnapshot('description', event.target.value)} rows={3} />
          </label>
          <label className="form-wide">
            Config JSON
            <textarea name="config" value={formSnapshot.config} onChange={(event) => updateSnapshot('config', event.target.value)} rows={10} aria-invalid={Boolean(fieldErrors.config)} />
            {fieldErrors.config ? <span className="error-text">{fieldErrors.config}</span> : null}
          </label>
          {fieldErrors.submit ? <div role="alert" className="error-box form-wide">{fieldErrors.submit}</div> : null}
          <div className="dialog-actions form-wide">
            <Link to="/strategies" className="secondary-link">Cancel</Link>
            <button type="submit" disabled={mutation.isPending}>Review paper create</button>
          </div>
        </form>
      </section>
      <ConfirmationDialog
        open={dialogOpen}
        title="Create paper strategy?"
        confirmLabel="Create paper strategy"
        busy={mutation.isPending}
        disableDismiss={mutation.isPending}
        error={dialogError ? <>{dialogError.message}{dialogError.unknownCompletion ? <strong> Do not retry until the strategy list is checked.</strong> : null}</> : null}
        onCancel={() => { if (!mutation.isPending) setDialogOpen(false) }}
        onConfirm={() => { if (pendingRequest && !mutation.isPending) mutation.mutate(pendingRequest) }}
      >
        <p><strong>{pendingRequest?.name}</strong> ({pendingRequest?.ticker}) will be created as <strong>PAPER only</strong>.</p>
        <p>No optimistic row will be shown. The UI will verify the created strategy before navigating to detail.</p>
      </ConfirmationDialog>
    </div>
  )
}
