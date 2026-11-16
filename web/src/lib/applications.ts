import type { Application, ApplicationStatus } from '../api'

export const APPLICATION_STATUSES: { value: ApplicationStatus; label: string }[] = [
  { value: 'applied', label: 'Applied' },
  { value: 'interviewing', label: 'Interviewing' },
  { value: 'offer', label: 'Offer' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'withdrawn', label: 'Withdrawn' },
]

export function statusLabel(status: ApplicationStatus): string {
  return APPLICATION_STATUSES.find((s) => s.value === status)?.label ?? status
}

/** The badge colour for a status. */
export function statusTone(status: ApplicationStatus): string {
  switch (status) {
    case 'applied':
      return 'b2b'
    case 'interviewing':
      return 'warn'
    case 'offer':
      return 'ok'
    case 'rejected':
      return 'danger'
    default:
      return ''
  }
}

/** Today's date in the browser's time zone, as YYYY-MM-DD. */
export function today(now = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

/** "12 Nov 2026" for a YYYY-MM-DD date, without time-zone drift. */
export function formatDay(day: string): string {
  const [y, m, d] = day.split('-').map(Number)
  if (!y || !m || !d) return day
  return new Date(y, m - 1, d).toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })
}

/** "Applied 12 Nov 2026", "Interviewing · since 12 Nov 2026". */
export function applicationSummary(a: Application): string {
  return a.status === 'applied' ? `Applied ${formatDay(a.applied_on)}` : `${statusLabel(a.status)} · applied ${formatDay(a.applied_on)}`
}

/** Whether the form differs from what is stored. */
export function applicationDirty(form: { status: string; applied_on: string; notes: string }, a: Application): boolean {
  return form.status !== a.status || form.applied_on !== a.applied_on || form.notes.trim() !== a.notes.trim()
}
