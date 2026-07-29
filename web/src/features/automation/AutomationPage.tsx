import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'

import { PageHeader } from '@/components/ui/page-header'
import { StatusBadge } from '@/components/ui/status-badge'
import { getAutomationHealth, getAutomationStatus, runAutomationJob, setAutomationJobEnabled } from '@/shared/api/endpoints'
import { EmptyState, ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { AutomationJobStatus } from '@/shared/types/domain'

function formatRelativeTime(iso?: string): string {
  if (!iso) return 'Never'
  const diff = Date.now() - new Date(iso).getTime()
  const seconds = Math.max(0, Math.floor(diff / 1000))
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}

function JobStatePill({ job }: { job: AutomationJobStatus }) {
  if (!job.enabled) return <StatusBadge status="unknown" label="disabled" />
  if (job.running) return <StatusBadge status="running" />
  if (job.consecutive_failures >= 3) return <StatusBadge status="danger" label="failing" />
  if (job.consecutive_failures > 0) return <StatusBadge status="warning" label="degraded" />
  return <StatusBadge status="success" label="healthy" />
}

function AutomationActions({ job }: { job: AutomationJobStatus }) {
  const queryClient = useQueryClient()
  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.automationStatus }),
      queryClient.invalidateQueries({ queryKey: queryKeys.automationHealth }),
      queryClient.invalidateQueries({ queryKey: queryKeys.automationRuns({ limit: 50, offset: 0 }) }),
    ])
  }
  const runMutation = useMutation({ mutationFn: () => runAutomationJob(job.name), onSuccess: invalidate })
  const toggleMutation = useMutation({ mutationFn: () => setAutomationJobEnabled(job.name, !job.enabled), onSuccess: invalidate })
  const busy = runMutation.isPending || toggleMutation.isPending

  return (
    <div className="table-actions">
      <button type="button" className="compact-button" disabled={busy || job.running || !job.enabled} onClick={() => runMutation.mutate()}>
        {runMutation.isPending ? 'Running…' : 'Run now'}
      </button>
      <button type="button" className="compact-button secondary-button" disabled={busy || job.running} onClick={() => toggleMutation.mutate()}>
        {toggleMutation.isPending ? 'Saving…' : job.enabled ? 'Disable' : 'Enable'}
      </button>
    </div>
  )
}

export function AutomationPage() {
  const statusQuery = useQuery({ queryKey: queryKeys.automationStatus, queryFn: ({ signal }) => getAutomationStatus(signal), refetchInterval: 30_000 })
  const healthQuery = useQuery({ queryKey: queryKeys.automationHealth, queryFn: ({ signal }) => getAutomationHealth(signal), refetchInterval: 30_000 })
  const jobs = statusQuery.data ?? []
  const orderedJobs = [...jobs].sort((a, b) => {
    const priority = (job: AutomationJobStatus) => job.running ? 0 : job.consecutive_failures >= 3 ? 1 : job.consecutive_failures > 0 ? 2 : job.enabled ? 3 : 4
    return priority(a) - priority(b) || a.name.localeCompare(b.name)
  })
  const attentionJobs = orderedJobs.filter((job) => job.running || job.consecutive_failures > 0)

  return (
    <div className="detail-stack">
      <PageHeader eyebrow="Paper operations" title="Automations" description="What is running, what needs attention, and when each paper-trading job last completed." actions={<LastUpdated date={statusQuery.dataUpdatedAt || undefined} />} />

      <section className="panel operations-hero">
        <div><p className="eyebrow">Scheduler state</p><h2>{healthQuery.data?.healthy ? 'Automation is operating normally' : 'Automation needs attention'}</h2><p className="muted">{attentionJobs.length > 0 ? `${attentionJobs.length} job${attentionJobs.length === 1 ? '' : 's'} running or reporting failures. They are listed first below.` : 'No running or failed jobs currently require operator attention.'}</p></div>
        {healthQuery.data ? (
          <div className="operations-metrics" aria-label="Automation summary">
            <div><span>Registered</span><strong>{healthQuery.data.total_jobs}</strong></div><div><span>Failing</span><strong>{healthQuery.data.failing_jobs}</strong></div><div><span>Degraded</span><strong>{healthQuery.data.degraded_jobs}</strong></div><div><span>Overall</span><strong>{healthQuery.data.healthy ? 'Healthy' : 'Degraded'}</strong></div>
          </div>
        ) : null}
      </section>

      <section className="panel">
        <div className="panel-header"><div><p className="eyebrow">Execution queue</p><h2>Scheduled jobs</h2><p className="muted">Running and unhealthy jobs are pinned to the top.</p></div></div>
        {statusQuery.isLoading ? <LoadingState label="Loading automation jobs…" /> : null}
        {statusQuery.error ? <ErrorState error={statusQuery.error} onRetry={() => void statusQuery.refetch()} /> : null}
        {!statusQuery.isLoading && !statusQuery.error && jobs.length === 0 ? <EmptyState title="No automations found" message="The automation orchestrator is not reporting any registered jobs." /> : null}

        {jobs.length > 0 ? (
          <div className="table-wrap" role="region" aria-label="Automation jobs table" tabIndex={0}>
            <table className="operations-table automation-table" aria-label="Automation jobs">
              <thead>
                <tr>
                  <th scope="col">Job</th>
                  <th scope="col">State</th>
                  <th scope="col">Schedule</th>
                  <th scope="col">Last run</th>
                  <th scope="col">Runs</th>
                  <th scope="col">Errors</th>
                  <th scope="col">Actions</th>
                </tr>
              </thead>
              <tbody>
                {orderedJobs.map((job) => (
                  <tr key={job.name}>
                    <th scope="row">
                      <Link to={`/automation/${encodeURIComponent(job.name)}`}>{job.name}</Link>
                      <p className="cell-detail" title={job.description}>{job.description}</p>
                    </th>
                    <td>
                      <JobStatePill job={job} />
                      {job.settlement_gate ? <span className="cell-detail">{job.settlement_gate.eligible ? 'Gate ready' : `Gate ${job.settlement_gate.consecutive_dry_run_successes}/${job.settlement_gate.threshold}`} · {job.settlement_gate.would_settle_decisions} pending</span> : null}
                    </td>
                    <td>{job.schedule || 'Manual only'}</td>
                    <td>{formatRelativeTime(job.last_run)}</td>
                    <td>{job.run_count}</td>
                    <td>{job.error_count > 0 ? <><StatusBadge status={job.consecutive_failures > 0 ? 'danger' : 'warning'} label={`${job.error_count} historical`} />{job.consecutive_failures > 0 ? <span className="cell-detail">{job.consecutive_failures} consecutive</span> : null}{job.last_error ? <span className="cell-detail" title={job.last_error}>{job.last_error}</span> : null}</> : '0'}</td>
                    <td><AutomationActions job={job} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>
    </div>
  )
}
