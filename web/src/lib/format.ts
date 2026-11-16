const SOURCE_LABELS: Record<string, string> = {
  remoteok: 'RemoteOK',
  remotive: 'Remotive',
  himalayas: 'Himalayas',
  weworkremotely: 'We Work Remotely',
  jobicy: 'Jobicy',
  arbeitnow: 'Arbeitnow',
  hackernews: 'Hacker News',
  greenhouse: 'Greenhouse',
  lever: 'Lever',
  ashby: 'Ashby',
}

export function sourceLabel(source: string): string {
  return SOURCE_LABELS[source] ?? source
}

const REASON_LABELS: Record<string, string> = {
  'not remote': 'Not remote',
  region: 'Region',
  duplicate: 'Duplicate',
  role: 'Role',
  seniority: 'Seniority',
}

export function reasonLabel(reason: string): string {
  return REASON_LABELS[reason] ?? reason
}

/** What each drop reason means, for tooltips. */
export const REASON_HELP: Record<string, string> = {
  'not remote': 'Listed as on-site or hybrid',
  region: 'Remote, but restricted to places outside Romania/Europe',
  duplicate: 'Same company and title already seen from another source',
  role: 'Title is not a software engineering role (sales, design, support, management…)',
  seniority: 'Title says junior, intern or mid-level',
}

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['day', 86_400],
  ['hour', 3_600],
  ['minute', 60],
]
const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })

/** relativeTime renders an ISO time as "2 hours ago" / "in 5 minutes". */
export function relativeTime(iso: string | null, now: Date = new Date()): string {
  if (!iso) return 'never'
  const diff = (new Date(iso).getTime() - now.getTime()) / 1000
  if (Number.isNaN(diff)) return 'unknown'
  for (const [unit, secs] of UNITS) {
    if (Math.abs(diff) >= secs) return rtf.format(Math.round(diff / secs), unit)
  }
  return 'just now'
}

/** formatInterval renders 1800 as "30 min" and 21600 as "6 h". */
export function formatInterval(seconds: number): string {
  if (seconds <= 0) return '—'
  if (seconds % 3600 === 0) return `${seconds / 3600} h`
  if (seconds >= 3600) return `${(seconds / 3600).toFixed(1)} h`
  return `${Math.round(seconds / 60)} min`
}

export function formatDuration(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${ms} ms`
}

const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 0 })

export function formatSalary(min?: number, max?: number, currency?: string): string | null {
  if (!min && !max) return null
  const cur = currency ? `${currency} ` : ''
  if (min && max && min !== max) return `${cur}${compact.format(min)}–${compact.format(max)}`
  return `${cur}${compact.format(max || min || 0)}`
}

/** Where a job is open, in one short string. */
export function locationSummary(location: string, regions?: string[]): string {
  const parts = regions && regions.length > 0 ? regions : location ? [location] : []
  if (parts.length === 0) return 'No location given'
  const text = parts.join(' · ')
  return text.length > 70 ? `${text.slice(0, 67)}…` : text
}

/** When the next poll of a source is due, relative to now. */
export function nextDue(startedAt: string | null, intervalSeconds: number, now: Date = new Date()): string {
  if (!startedAt) return 'next tick'
  const due = new Date(startedAt).getTime() + intervalSeconds * 1000
  if (due <= now.getTime()) return 'due now'
  return relativeTime(new Date(due).toISOString(), now)
}
