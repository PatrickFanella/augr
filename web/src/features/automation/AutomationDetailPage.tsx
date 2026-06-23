import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'

import { PageHeader } from '@/components/ui/page-header'
import { StatusBadge } from '@/components/ui/status-badge'
import { getAutomationRuns, getAutomationStatus, runAutomationJob, setAutomationJobEnabled } from '@/shared/api/endpoints'
import { EmptyState, ErrorState, LastUpdated, LoadingState } from '@/shared/components/QueryStates'
import { queryKeys } from '@/shared/query/keys'
import type { AutomationJobStatus } from '@/shared/types/domain'
import { normalizeStatus } from '@/lib/status'

function formatDuration(ns?: number): string {
  if (!ns) return '--'
  const ms = Math.round(ns / 1_000_000)
  if (ms < 1_000) return `${ms}ms`
  const seconds = Math.round(ms / 1_000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.round(seconds / 60)}m`
}

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString() : '--'
}

function JobStatePill({ job }: { job: AutomationJobStatus }) {
  if (!job.enabled) return <StatusBadge status="unknown" label="disabled" />
  if (job.running) return <StatusBadge status="running" />
  if (job.consecutive_failures >= 3) return <StatusBadge status="danger" label="failing" />
  if (job.consecutive_failures > 0) return <StatusBadge status="warning" label="degraded" />
  return <StatusBadge status="success" label="healthy" />
}

export function AutomationDetailPage() {
  const params = useParams()
  const name = params.name ?? ''
  const queryClient = useQueryClient()
  const statusQuery = useQuery({ queryKey: queryKeys.automationStatus, queryFn: ({ signal }) => getAutomationStatus(signal), refetchInterval: 30_000 })
  const runsQuery = useQuery({ queryKey: queryKeys.automationRuns({ limit: 100, offset: 0 }), queryFn: ({ signal }) => getAutomationRuns({ limit: 100, offset: 0 }, signal), refetchInterval: 30_000 })
  const job = statusQuery.data?.find((item) => item.name === name)
  const runs = (runsQuery.data?.data ?? []).filter((run) => run.job_name === name)

  const invalidate = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.automationStatus }),
      queryClient.invalidateQueries({ queryKey: queryKeys.automationHealth }),
      queryClient.invalidateQueries({ queryKey: queryKeys.automationRuns({ limit: 100, offset: 0 }) }),
    ])
  }
  const runMutation = useMutation({ mutationFn: () => runAutomationJob(name), onSuccess: invalidate })
  const toggleMutation = useMutation({ mutationFn: () => setAutomationJobEnabled(name, !(job?.enabled ?? false)), onSuccess: invalidate })
  const busy = runMutation.isPending || toggleMutation.isPending

  return (
    <div className="detail-stack">
      <PageHeader eyebrow="Automation" title={name} description="Job status, controls, and recent execution history." actions={<LastUpdated date={statusQuery.dataUpdatedAt || undefined} />} />

      <section className="panel">

        {statusQuery.isLoading ? <LoadingState label="Loading automation job…" /> : null}
        {statusQuery.error ? <ErrorState error={statusQuery.error} onRetry={() => void statusQuery.refetch()} /> : null}
        {!statusQuery.isLoading && !statusQuery.error && !job ? <EmptyState title="Automation not found" message="No registered job matches this name." /> : null}

        {job ? (
          <>
            <div className="metrics-grid">
              <div className="panel nested-panel"><span>State</span><strong><JobStatePill job={job} /></strong></div>
              <div className="panel nested-panel"><span>Runs</span><strong>{job.run_count}</strong></div>
              <div className="panel nested-panel"><span>Errors</span><strong>{job.error_count}</strong></div>
              <div className="panel nested-panel"><span>Consecutive failures</span><strong>{job.consecutive_failures}</strong></div>
            </div>
            <dl className="detail-grid">
              <div><dt>Description</dt><dd>{job.description}</dd></div>
              <div><dt>Schedule</dt><dd>{job.schedule || 'Manual only'}</dd></div>
              <div><dt>Last run</dt><dd>{formatDate(job.last_run)}</dd></div>
              <div><dt>Last result</dt><dd>{job.last_result || '--'}</dd></div>
              <div><dt>Last error</dt><dd>{job.last_error || '--'}</dd></div>
            </dl>
            {job.last_summary ? (
              <div className="json-viewer"><pre>{JSON.stringify(job.last_summary, null, 2)}</pre></div>
            ) : null}
            <div className="header-cluster">
              <button type="button" disabled={busy || job.running || !job.enabled} onClick={() => runMutation.mutate()}>{runMutation.isPending ? 'Running…' : 'Run now'}</button>
              <button type="button" disabled={busy || job.running} onClick={() => toggleMutation.mutate()}>{toggleMutation.isPending ? 'Saving…' : job.enabled ? 'Disable' : 'Enable'}</button>
            </div>
          </>
        ) : null}
      </section>

      <section className="panel">
        <div className="panel-header">
          <div>
            <p className="eyebrow">History</p>
            <h2>Recent runs</h2>
          </div>
          <LastUpdated date={runsQuery.dataUpdatedAt || undefined} />
        </div>
        {runsQuery.isLoading ? <LoadingState label="Loading automation history…" /> : null}
        {runsQuery.error ? <ErrorState error={runsQuery.error} onRetry={() => void runsQuery.refetch()} /> : null}
        {!runsQuery.isLoading && !runsQuery.error && runs.length === 0 ? <EmptyState title="No recent runs" message="This job has no recent persisted run history in the latest 100 automation job runs." /> : null}
        {runs.length > 0 ? (
          <div className="table-wrap" role="region" aria-label="Automation run history" tabIndex={0}>
            <table aria-label="Automation run history">
              <thead><tr><th>Status</th><th>Started</th><th>Completed</th><th>Duration</th><th>Error</th></tr></thead>
              <tbody>
                {runs.map((run) => (
                  <tr key={run.id}>
                    <td><StatusBadge status={normalizeStatus(run.status)} label={run.status} /></td>
                    <td>{formatDate(run.started_at)}</td>
                    <td>{formatDate(run.completed_at)}</td>
                    <td>{formatDuration(run.duration_ns)}</td>
                    <td>{run.error || '--'}</td>
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
