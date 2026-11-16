import type { Application } from '../api'
import { applicationDirty, applicationSummary, formatDay, statusLabel, statusTone, today } from './applications'

const app = (over: Partial<Application> = {}): Application => ({
  status: 'applied', applied_on: '2026-11-12', notes: '', updated_at: '2026-11-12T09:00:00Z', ...over,
})

describe('today', () => {
  it('is the local date, zero-padded', () => expect(today(new Date(2026, 0, 5, 23, 59))).toBe('2026-01-05'))
})

describe('formatDay', () => {
  it('formats a date without shifting it across time zones', () => expect(formatDay('2026-11-01')).toBe('1 Nov 2026'))
  it('leaves something that is not a date alone', () => expect(formatDay('soon')).toBe('soon'))
})

describe('applicationSummary', () => {
  it('says when you applied', () => expect(applicationSummary(app())).toBe('Applied 12 Nov 2026'))
  it('leads with the stage once it has moved on', () =>
    expect(applicationSummary(app({ status: 'interviewing' }))).toBe('Interviewing · applied 12 Nov 2026'))
})

describe('status helpers', () => {
  it('has a label for every status', () => expect(statusLabel('withdrawn')).toBe('Withdrawn'))
  it('colours good news green and bad news red', () => {
    expect(statusTone('offer')).toBe('ok')
    expect(statusTone('rejected')).toBe('danger')
    expect(statusTone('withdrawn')).toBe('')
  })
})

describe('applicationDirty', () => {
  const same = { status: 'applied', applied_on: '2026-11-12', notes: '' }
  it('is clean when nothing changed, ignoring whitespace in notes', () => {
    expect(applicationDirty(same, app())).toBe(false)
    expect(applicationDirty({ ...same, notes: '  ' }, app())).toBe(false)
  })
  it.each([
    ['status', { status: 'offer' }],
    ['date', { applied_on: '2026-11-13' }],
    ['notes', { notes: 'call on Monday' }],
  ])('notices a change to %s', (_, change) => expect(applicationDirty({ ...same, ...change }, app())).toBe(true))
})
