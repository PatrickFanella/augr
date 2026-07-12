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
    <div className="header-cluster">
      <button type="button" disabled={busy || job.running || !job.enabled} onClick={() => runMutation.mutate()}>
        {runMutation.isPending ? 'Running…' : 'Run now'}
      </button>
      <button type="button" disabled={busy || job.running} onClick={() => toggleMutation.mutate()}>
        {toggleMutation.isPending ? 'Saving…' : job.enabled ? 'Disable' : 'Enable'}
      </button>
    </div>
  )
}

export function AutomationPage() {
  const statusQuery = useQuery({ queryKey: queryKeys.automationStatus, queryFn: ({ signal }) => getAutomationStatus(signal), refetchInterval: 30_000 })
  const healthQuery = useQuery({ queryKey: queryKeys.automationHealth, queryFn: ({ signal }) => getAutomationHealth(signal), refetchInterval: 30_000 })
  const jobs = statusQuery.data ?? []

  return (
    <div className="detail-stack">
      <PageHeader eyebrow="Automation" title="Automations" description="Scheduled jobs like deep_scan, hot_scan, reconciles, reports, and portfolio allocation." actions={<LastUpdated date={statusQuery.dataUpdatedAt || undefined} />} />

      <section className="panel">

        {healthQuery.data ? (
          <div className="metrics-grid">
            <div className="panel nested-panel"><span>Total jobs</span><strong>{healthQuery.data.total_jobs}</strong></div>
            <div className="panel nested-panel"><span>Failing</span><strong>{healthQuery.data.failing_jobs}</strong></div>
            <div className="panel nested-panel"><span>Degraded</span><strong>{healthQuery.data.degraded_jobs}</strong></div>
            <div className="panel nested-panel"><span>Overall</span><strong>{healthQuery.data.healthy ? 'Healthy' : 'Degraded'}</strong></div>
          </div>
        ) : null}

        {statusQuery.isLoading ? <LoadingState label="Loading automation jobs…" /> : null}
        {statusQuery.error ? <ErrorState error={statusQuery.error} onRetry={() => void statusQuery.refetch()} /> : null}
        {!statusQuery.isLoading && !statusQuery.error && jobs.length === 0 ? <EmptyState title="No automations found" message="The automation orchestrator is not reporting any registered jobs." /> : null}

        {jobs.length > 0 ? (
          <div className="table-wrap" role="region" aria-label="Automation jobs table" tabIndex={0}>
            <table aria-label="Automation jobs">
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
                {jobs.map((job) => (
                  <tr key={job.name}>
                    <th scope="row">
                      <Link to={`/automation/${encodeURIComponent(job.name)}`}>{job.name}</Link>
                      <p className="muted">{job.description}</p>
                    </th>
                    <td><JobStatePill job={job} /></td>
                    <td>{job.schedule || 'Manual only'}</td>
                    <td>{formatRelativeTime(job.last_run)}</td>
                    <td>{job.run_count}</td>
                    <td>{job.error_count > 0 ? <StatusBadge status="danger" label={String(job.error_count)} /> : job.error_count}</td>
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
