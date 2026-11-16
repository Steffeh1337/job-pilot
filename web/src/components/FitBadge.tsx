import type { Fit } from '../api'
import { bandLabel, fitTooltip } from '../lib/fit'

/** The match score for a job, coloured by band. */
export function FitBadge({ fit }: { fit: Fit }) {
  return (
    <span className={`fit ${fit.band}`} title={fitTooltip(fit)}>
      <b className="num">{fit.score}</b>
      {bandLabel(fit.band)}
    </span>
  )
}
