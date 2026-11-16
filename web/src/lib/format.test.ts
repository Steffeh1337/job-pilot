import {
  formatDuration, formatInterval, formatSalary, locationSummary, nextDue, reasonLabel, relativeTime, sourceLabel,
} from './format'

const now = new Date('2026-11-10T12:00:00Z')

describe('relativeTime', () => {
  it.each([
    ['2026-11-10T10:00:00Z', '2 hours ago'],
    ['2026-11-10T12:05:00Z', 'in 5 minutes'],
    ['2026-11-10T11:59:40Z', 'just now'],
    ['2026-11-09T12:00:00Z', 'yesterday'],
    ['2026-11-03T12:00:00Z', '7 days ago'],
  ])('%s -> %s', (iso, want) => expect(relativeTime(iso, now)).toBe(want))

  it('says never for a missing time', () => expect(relativeTime(null, now)).toBe('never'))
  it('survives garbage', () => expect(relativeTime('nope', now)).toBe('unknown'))
})

describe('nextDue', () => {
  it('is due now once the interval has passed', () =>
    expect(nextDue('2026-11-10T10:00:00Z', 3600, now)).toBe('due now'))
  it('counts down inside the interval', () =>
    expect(nextDue('2026-11-10T11:30:00Z', 3600, now)).toBe('in 30 minutes'))
  it('waits for the next tick when never polled', () => expect(nextDue(null, 3600, now)).toBe('next tick'))
})

describe('formatInterval', () => {
  it.each([[1800, '30 min'], [3600, '1 h'], [21600, '6 h'], [5400, '1.5 h'], [0, '—']])('%s -> %s', (s, want) =>
    expect(formatInterval(s)).toBe(want))
})

describe('formatDuration', () => {
  it('uses ms below a second', () => expect(formatDuration(317)).toBe('317 ms'))
  it('uses seconds above', () => expect(formatDuration(11547)).toBe('11.5 s'))
})

describe('formatSalary', () => {
  it('formats a range', () => expect(formatSalary(150000, 250000, 'EUR')).toBe('EUR 150K–250K'))
  it('formats a single value', () => expect(formatSalary(0, 90000, 'USD')).toBe('USD 90K'))
  it('collapses an equal range', () => expect(formatSalary(90000, 90000)).toBe('90K'))
  it('is null without data', () => expect(formatSalary(0, 0, 'EUR')).toBeNull())
})

describe('locationSummary', () => {
  it('prefers structured regions', () => expect(locationSummary('Remote', ['EMEA', 'USA'])).toBe('EMEA · USA'))
  it('falls back to free text', () => expect(locationSummary('Remote - Europe')).toBe('Remote - Europe'))
  it('says so when there is nothing', () => expect(locationSummary('')).toBe('No location given'))
  it('truncates very long text', () => expect(locationSummary('x'.repeat(200)).length).toBe(68))
})

describe('labels', () => {
  it('knows the sources and falls back to the raw name', () => {
    expect(sourceLabel('weworkremotely')).toBe('We Work Remotely')
    expect(sourceLabel('newboard')).toBe('newboard')
  })
  it('knows the reasons', () => {
    expect(reasonLabel('not remote')).toBe('Not remote')
    expect(reasonLabel('other')).toBe('other')
  })
})
