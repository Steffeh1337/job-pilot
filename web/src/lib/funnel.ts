import type { FunnelRow } from '../api'

export const STEPS = [
  { key: 'new', label: 'Seen' },
  { key: 'unique', label: 'Unique' },
  { key: 'region', label: 'Remote, open to Romania' },
  { key: 'role', label: 'Software engineering role' },
  { key: 'senior', label: 'Senior match' },
] as const

export type StepKey = (typeof STEPS)[number]['key']

export interface Step {
  key: StepKey
  label: string
  count: number
  /** Share of the previous step that survived; null for the first step. */
  ofPrev: number | null
  /** Share of everything seen. */
  ofFirst: number
}

/** pct returns n/d as a percentage, or 0 when d is 0. */
export function pct(n: number, d: number): number {
  return d === 0 ? 0 : (n / d) * 100
}

export function stepsFor(row: FunnelRow): Step[] {
  return STEPS.map((s, i) => {
    const count = row[s.key]
    const prev = i === 0 ? null : row[STEPS[i - 1]!.key]
    return {
      key: s.key,
      label: s.label,
      count,
      ofPrev: prev === null ? null : pct(count, prev),
      ofFirst: pct(count, row.new),
    }
  })
}

/** The row named TOTAL that the API appends, or an all-zero row. */
export function totalRow(rows: FunnelRow[]): FunnelRow {
  return (
    rows.find((r) => r.source === 'TOTAL') ?? {
      source: 'TOTAL', new: 0, unique: 0, region: 0, role: 0, senior: 0, b2b: 0,
    }
  )
}

export function sourceRows(rows: FunnelRow[]): FunnelRow[] {
  return rows.filter((r) => r.source !== 'TOTAL')
}

export function formatPct(value: number): string {
  if (value === 0) return '0%'
  if (value < 1) return '<1%'
  return `${Math.round(value)}%`
}
