import type { JobQuery, Status } from '../api'

export const PAGE_SIZE = 25

const STATUSES: Status[] = ['match', 'rejected', 'all']

/** Reads the Jobs page filters from the URL, tolerating junk values. */
export function queryFromParams(p: URLSearchParams): JobQuery & { page: number } {
  const status = p.get('status') as Status
  const page = Math.max(1, Number.parseInt(p.get('page') ?? '1', 10) || 1)
  const days = Number.parseInt(p.get('days') ?? '', 10)
  return {
    status: STATUSES.includes(status) ? status : 'match',
    source: p.get('source') || undefined,
    reason: p.get('reason') || undefined,
    b2b: p.get('b2b') === '1',
    unverified: p.get('unverified') === '1',
    q: p.get('q') || undefined,
    days: days > 0 ? days : undefined,
    page,
    limit: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE,
  }
}

/** Builds URL search params for the Jobs page, dropping default values. */
export function paramsFromQuery(q: Partial<JobQuery> & { page?: number }): URLSearchParams {
  const p = new URLSearchParams()
  if (q.status && q.status !== 'match') p.set('status', q.status)
  if (q.source) p.set('source', q.source)
  if (q.reason) p.set('reason', q.reason)
  if (q.b2b) p.set('b2b', '1')
  if (q.unverified) p.set('unverified', '1')
  if (q.q) p.set('q', q.q)
  if (q.days) p.set('days', String(q.days))
  if (q.page && q.page > 1) p.set('page', String(q.page))
  return p
}
