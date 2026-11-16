import { Link } from 'react-router-dom'
import type { Fit } from '../api'
import { semanticPercent } from '../lib/fit'
import { FitBadge } from './FitBadge'

/** Why a job scored as it did: the skills in common, the ones missing, and wording. */
export function FitSection({ fit }: { fit: Fit }) {
  const semantic = semanticPercent(fit)
  const total = fit.matched.length + fit.missing.length
  return (
    <section className="fit-section" aria-labelledby="fit-heading">
      <div className="fit-head">
        <h3 id="fit-heading">Fit with your CV</h3>
        <FitBadge fit={fit} />
      </div>

      {total === 0 ? (
        <p className="muted small">This posting doesn’t name any skills we recognise, so only its wording is compared.</p>
      ) : (
        <>
          {fit.matched.length > 0 && (
            <div className="skill-group">
              <div className="small muted">On your CV ({fit.matched.length})</div>
              <ul className="chips" aria-label="Matched skills">
                {fit.matched.map((s) => (
                  <li key={s} className="chip matched">
                    {s}
                  </li>
                ))}
              </ul>
            </div>
          )}
          {fit.missing.length > 0 && (
            <div className="skill-group">
              <div className="small muted">Not on your CV ({fit.missing.length})</div>
              <ul className="chips" aria-label="Missing skills">
                {fit.missing.map((s) => (
                  <li key={s} className="chip missing">
                    {s}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}

      {semantic !== null ? (
        <p className="small">
          Wording similarity: <strong>{semantic}%</strong>{' '}
          <span className="muted">(how close the posting reads to your CV)</span>
        </p>
      ) : (
        <p className="small muted">
          Based on skills only. Start Ollama to add a wording-similarity score.{' '}
          <Link to="/profile">Set it up</Link>
        </p>
      )}
    </section>
  )
}
