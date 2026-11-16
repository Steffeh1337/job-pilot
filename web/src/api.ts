// Types mirror the JSON served by backend/internal/httpapi.

export interface FunnelRow {
  source: string
  new: number
  unique: number
  region: number
  role: number
  stack: number
  senior: number
  b2b: number
}

export interface FunnelResponse {
  days: number
  since: string
  rows: FunnelRow[]
  rejected: Record<string, number>
}

export type Status = 'match' | 'rejected' | 'all'

export interface Job {
  id: number
  source: string
  url: string
  title: string
  company: string
  location: string
  regions?: string[]
  remote: boolean
  employment_type?: string
  tags?: string[]
  b2b: boolean
  region_verified: boolean
  stage: string
  reject_reason?: string
  salary_min?: number
  salary_max?: number
  currency?: string
  posted_at: string
  first_seen_at: string
  description?: string
}

export interface JobsResponse {
  total: number
  jobs: Job[]
}

export interface Poll {
  source: string
  started_at: string | null
  duration_ms: number
  fetched: number
  new_jobs: number
  error?: string
  interval_seconds: number
}

export interface PollResult {
  source: string
  fetched: number
  new_jobs: number
  /** Left alone because it was polled very recently. */
  skipped?: boolean
  error?: string
}

export interface RefreshStatus {
  running: boolean
  started_at: string | null
  finished_at: string | null
  new_jobs: number
  new_matches: number
  results: PollResult[]
}

export interface JobQuery {
  status: Status
  source?: string
  reason?: string
  b2b?: boolean
  unverified?: boolean
  q?: string
  days?: number
  limit?: number
  offset?: number
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { ...init, headers: { Accept: 'application/json' } })
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // not JSON; keep the status line
    }
    throw new Error(message)
  }
  return (await res.json()) as T
}

const getJSON = <T>(path: string) => request<T>(path)

export function jobsPath(q: JobQuery): string {
  const p = new URLSearchParams()
  p.set('status', q.status)
  if (q.source) p.set('source', q.source)
  if (q.reason) p.set('reason', q.reason)
  if (q.b2b) p.set('b2b', '1')
  if (q.unverified) p.set('unverified', '1')
  if (q.q) p.set('q', q.q)
  if (q.days) p.set('days', String(q.days))
  if (q.limit) p.set('limit', String(q.limit))
  if (q.offset) p.set('offset', String(q.offset))
  return `/api/jobs?${p}`
}

export const api = {
  funnel: (days: number) => getJSON<FunnelResponse>(`/api/funnel?days=${days}`),
  jobs: (q: JobQuery) => getJSON<JobsResponse>(jobsPath(q)),
  job: (id: number) => getJSON<Job>(`/api/jobs/${id}`),
  polls: () => getJSON<Poll[]>('/api/polls'),
  refreshStatus: () => getJSON<RefreshStatus>('/api/refresh'),
  startRefresh: () => request<RefreshStatus>('/api/refresh', { method: 'POST' }),
}
