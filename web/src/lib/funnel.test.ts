import type { FunnelRow } from '../api'
import { formatPct, pct, sourceRows, stepsFor, totalRow } from './funnel'

const row = (over: Partial<FunnelRow> = {}): FunnelRow => ({
  source: 'x', new: 200, unique: 100, region: 50, role: 20, senior: 5, b2b: 1, ...over,
})

describe('pct', () => {
  it('computes a percentage', () => expect(pct(1, 4)).toBe(25))
  it('is 0 when the denominator is 0', () => expect(pct(5, 0)).toBe(0))
})

describe('stepsFor', () => {
  it('reports survival against the previous step and against everything seen', () => {
    const steps = stepsFor(row())
    expect(steps.map((s) => s.count)).toEqual([200, 100, 50, 20, 5])
    expect(steps[0]!.ofPrev).toBeNull()
    expect(steps[1]!.ofPrev).toBe(50)
    expect(steps[3]!.ofPrev).toBe(40)
    expect(steps[4]!.ofFirst).toBe(2.5)
  })

  it('handles a source with nothing in it', () => {
    const steps = stepsFor(row({ new: 0, unique: 0, region: 0, role: 0, senior: 0 }))
    expect(steps.every((s) => s.count === 0 && s.ofFirst === 0)).toBe(true)
    expect(steps[1]!.ofPrev).toBe(0)
  })
})

describe('rows', () => {
  const rows = [row({ source: 'a' }), row({ source: 'TOTAL', new: 400 })]
  it('finds the TOTAL row', () => expect(totalRow(rows).new).toBe(400))
  it('falls back to zeros without one', () => expect(totalRow([]).senior).toBe(0))
  it('excludes TOTAL from per-source rows', () => expect(sourceRows(rows).map((r) => r.source)).toEqual(['a']))
})

describe('formatPct', () => {
  it.each([
    [0, '0%'],
    [0.4, '<1%'],
    [2.5, '3%'],
    [50, '50%'],
    [100, '100%'],
  ])('%s -> %s', (n, want) => expect(formatPct(n)).toBe(want))
})
