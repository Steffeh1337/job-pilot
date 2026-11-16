import type { Fit } from '../api'
import { bandLabel, fitTooltip, semanticPercent, skillsSummary } from './fit'

const fit = (over: Partial<Fit> = {}): Fit => ({
  score: 82, band: 'strong', matched: ['Go', 'Docker', 'PostgreSQL'], missing: ['Kafka', 'Redis'], ...over,
})

describe('skillsSummary', () => {
  it('counts matched against everything the job names', () => expect(skillsSummary(fit())).toBe('3 of 5 skills'))
  it('is singular for one skill', () =>
    expect(skillsSummary(fit({ matched: ['Go'], missing: [] }))).toBe('1 of 1 skill'))
  it('is null when the job names none', () => expect(skillsSummary(fit({ matched: [], missing: [] }))).toBeNull())
})

describe('semanticPercent', () => {
  it('rounds to a percentage', () => expect(semanticPercent(fit({ semantic: 0.634 }))).toBe(63))
  it('is null without a model', () => expect(semanticPercent(fit())).toBeNull())
  it('keeps a real zero', () => expect(semanticPercent(fit({ semantic: 0 }))).toBe(0))
})

describe('fitTooltip', () => {
  it('lists the parts that exist', () => {
    expect(fitTooltip(fit({ semantic: 0.5 }))).toBe('Strong fit (82/100) · 3 of 5 skills match · wording 50% similar')
    expect(fitTooltip(fit({ matched: [], missing: [] }))).toBe('Strong fit (82/100)')
  })
})

it('labels every band', () => {
  expect(['strong', 'good', 'fair', 'weak'].map((b) => bandLabel(b as Fit['band']))).toEqual(['Strong', 'Good', 'Fair', 'Weak'])
})
