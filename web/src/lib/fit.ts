import type { Fit, FitBand } from '../api'

export const BAND_LABEL: Record<FitBand, string> = {
  strong: 'Strong',
  good: 'Good',
  fair: 'Fair',
  weak: 'Weak',
}

export function bandLabel(band: FitBand): string {
  return BAND_LABEL[band] ?? band
}

/** The minimum-fit choices on the Jobs page. Values match the band thresholds. */
export const MIN_FIT_OPTIONS = [
  { value: 0, label: 'Any fit' },
  { value: 35, label: 'Fair or better' },
  { value: 55, label: 'Good or better' },
  { value: 75, label: 'Strong only' },
] as const

/** "3 of 5 skills" — how many of the skills the job names are on your CV. */
export function skillsSummary(fit: Fit): string | null {
  const total = fit.matched.length + fit.missing.length
  if (total === 0) return null
  return `${fit.matched.length} of ${total} skill${total === 1 ? '' : 's'}`
}

/** The wording-similarity score as a percentage, or null when it wasn't computed. */
export function semanticPercent(fit: Fit): number | null {
  return fit.semantic === undefined ? null : Math.round(fit.semantic * 100)
}

/** A one-line explanation for a tooltip. */
export function fitTooltip(fit: Fit): string {
  const parts = [`${bandLabel(fit.band)} fit (${fit.score}/100)`]
  const skills = skillsSummary(fit)
  if (skills) parts.push(`${skills} match`)
  const sem = semanticPercent(fit)
  if (sem !== null) parts.push(`wording ${sem}% similar`)
  return parts.join(' · ')
}
