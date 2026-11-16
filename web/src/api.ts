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
  /** How well the job matches the uploaded CV; absent without one. */
  fit?: Fit
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

export type FitBand = 'strong' | 'good' | 'fair' | 'weak'

export interface Fit {
  score: number
  band: FitBand
  /** Share of the job's skills you have, 0..1. Absent if the job names none we know. */
  skill_score?: number
  /** How close the wording is to your CV, 0..1. Absent without a local model. */
  semantic?: number
  similarity?: number
  matched: string[]
  missing: string[]
}

export interface ProfileSkill {
  name: string
  /** A skill you use day to day; it counts more than one you merely know. */
  core: boolean
  category?: string
}

export interface Profile {
  headline: string
  seniority: string
  years_experience: number
  skills: ProfileSkill[]
  languages: string[]
  summary: string
  /** Up to three bullets that show exceptional ability. */
  highlights: string[]
}

export interface ProfileView extends Profile {
  exists: boolean
  cv_filename?: string
  cv_chars?: number
  cv_preview?: string
  extracted_by?: string
  uploaded_at?: string
  updated_at?: string
  has_embedding: boolean
}

/** Bullets drafted for review; nothing is saved until the profile is. */
export interface HighlightsResponse {
  highlights: string[]
  warnings: string[]
}

export interface ProfileResponse {
  profile: ProfileView
  warnings: string[]
}

export interface FitStatus {
  ollama: {
    enabled: boolean
    reachable: boolean
    version?: string
    error?: string
    embed_model: string
    embed_installed: boolean
    chat_model?: string
    chat_installed: boolean
    installed: string[]
  }
  has_profile: boolean
  index: {
    eligible: number
    with_skills: number
    with_embeddings: number
    syncing: boolean
    last_sync: string | null
    last_error?: string
  }
}

export interface JobQuery {
  status: Status
  source?: string
  reason?: string
  b2b?: boolean
  unverified?: boolean
  q?: string
  days?: number
  sort?: 'new' | 'fit'
  minFit?: number
  limit?: number
  offset?: number
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, { ...init, headers: { Accept: 'application/json', ...init?.headers } })
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
  if (res.status === 204) return undefined as T
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
  if (q.sort === 'fit') p.set('sort', 'fit')
  if (q.minFit) p.set('min_fit', String(q.minFit))
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
  profile: () => getJSON<ProfileView>('/api/profile'),
  uploadCV: (file: File) => {
    const form = new FormData()
    form.append('cv', file)
    return request<ProfileResponse>('/api/profile/cv', { method: 'POST', body: form })
  },
  reextract: () => request<ProfileResponse>('/api/profile/reextract', { method: 'POST' }),
  suggestHighlights: () => request<HighlightsResponse>('/api/profile/highlights', { method: 'POST' }),
  saveProfile: (p: Profile) =>
    request<ProfileView>('/api/profile', {
      method: 'PUT',
      body: JSON.stringify(p),
      headers: { 'Content-Type': 'application/json' },
    }),
  deleteProfile: () => request<void>('/api/profile', { method: 'DELETE' }),
  fitStatus: () => getJSON<FitStatus>('/api/fit/status'),
  fitSync: () => request<FitStatus>('/api/fit/sync', { method: 'POST' }),
}
