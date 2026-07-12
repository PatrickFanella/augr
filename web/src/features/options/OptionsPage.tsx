import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'

import { PageHeader } from '@/components/ui/page-header'
import { getOptionsChain } from '@/shared/api/endpoints'
import { Breadcrumbs } from '@/shared/components/EntityLinks'
import { EmptyState, ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'

function number(value: number, digits = 2) {
  return value.toLocaleString(undefined, { maximumFractionDigits: digits })
}

export function OptionsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const underlying = (searchParams.get('underlying') ?? '').trim().toUpperCase()
  const expiry = searchParams.get('expiry') ?? ''
  const type = searchParams.get('type') ?? ''
  const query = useQuery({
    queryKey: ['options', 'chain', underlying, expiry, type],
    queryFn: ({ signal }) => getOptionsChain(underlying, { expiry: expiry || undefined, type: type || undefined }, signal),
    enabled: Boolean(underlying),
  })
  const update = (values: Record<string, string>) => {
    const next = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(values)) {
      if (value) next.set(key, value)
      else next.delete(key)
    }
    setSearchParams(next)
  }
  return (
    <div className="detail-stack">
      <Breadcrumbs items={[{ label: 'Cockpit', to: '/cockpit' }, { label: 'Options research' }]} />
      <PageHeader eyebrow="Derivatives research" title="Options chain" description="Read-only contract prices, liquidity, IV, and Greeks. Paper execution is automation-driven through validated strategy rules; live and manual options orders remain disabled." actions={<span className="status-pill unknown">Research only</span>} />
      <section className="panel" aria-labelledby="options-query-heading">
        <h2 id="options-query-heading">Chain query</h2>
        <form className="filter-bar" aria-label="Options chain filters" onSubmit={(event) => event.preventDefault()}>
          <label>Underlying<input value={underlying} onChange={(event) => update({ underlying: event.target.value.toUpperCase() })} placeholder="AAPL" /></label>
          <label>Expiry<input type="date" value={expiry} onChange={(event) => update({ expiry: event.target.value })} /></label>
          <label>Type<select value={type} onChange={(event) => update({ type: event.target.value })}><option value="">Calls and puts</option><option value="call">Calls</option><option value="put">Puts</option></select></label>
          <button type="button" onClick={() => setSearchParams(new URLSearchParams())}>Clear</button>
        </form>
        {!underlying ? <EmptyState title="Choose an underlying" message="Enter a ticker to request its nearest available options chain." /> : null}
        {query.isLoading ? <LoadingState label={`Loading ${underlying} options chain…`} /> : null}
        {query.error ? <ErrorState error={query.error} onRetry={() => void query.refetch()} /> : null}
        {query.data && query.data.length === 0 ? <EmptyState title="No contracts returned" message="The provider returned no contracts for these filters." /> : null}
        {query.data?.length ? <><LastUpdated date={query.dataUpdatedAt} /><div className="table-wrap"><table aria-label={`${underlying} options chain`}><thead><tr><th>Contract</th><th>Type</th><th>Expiry</th><th>Strike</th><th>Bid</th><th>Ask</th><th>Mid</th><th>IV</th><th>Delta</th><th>Volume</th><th>Open interest</th></tr></thead><tbody>{query.data.map((snapshot) => <tr key={snapshot.contract.occ_symbol}><th scope="row">{snapshot.contract.occ_symbol}</th><td>{snapshot.contract.option_type}</td><td>{new Date(snapshot.contract.expiry).toLocaleDateString()}</td><td>{number(snapshot.contract.strike)}</td><td>{number(snapshot.bid)}</td><td>{number(snapshot.ask)}</td><td>{number(snapshot.mid)}</td><td>{number(snapshot.greeks.iv * 100)}%</td><td>{number(snapshot.greeks.delta, 4)}</td><td>{number(snapshot.volume, 0)}</td><td>{number(snapshot.open_interest, 0)}</td></tr>)}</tbody></table></div></> : null}
      </section>
    </div>
  )
}
