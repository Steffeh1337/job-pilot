import type { FunnelRow } from '../api'
import { formatPct, stepsFor } from '../lib/funnel'

/** The funnel as horizontal bars, each scaled against everything seen. */
export function FunnelChart({ row }: { row: FunnelRow }) {
  const steps = stepsFor(row)
  return (
    <ol className="funnel" aria-label="Funnel from listings seen to senior matches">
      {steps.map((s) => (
        <li key={s.key}>
          <span className="label">{s.label}</span>
          <div className="track" aria-hidden="true">
            <div className="bar" style={{ width: `${Math.max(s.ofFirst, s.count > 0 ? 0.5 : 0)}%` }} />
          </div>
          <span className="figures num">
            <strong>{s.count.toLocaleString()}</strong>
            {s.ofPrev !== null && <span title="Share of the previous step that survived">{formatPct(s.ofPrev)}</span>}
          </span>
        </li>
      ))}
    </ol>
  )
}
