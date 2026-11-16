import type { RefreshOutcome } from '../lib/useRefresh'
import { sourceLabel } from '../lib/format'

/** "1 job", "3 matches": adds -es after sibilants such as -ch, -s, -sh, -x. */
const plural = (n: number, word: string) => {
  const suffix = /(?:ch|sh|s|x)$/.test(word) ? 'es' : 's'
  return `${n.toLocaleString()} ${word}${n === 1 ? '' : suffix}`
}

/** Summarises what a refresh found, and anything that went wrong. */
export function RefreshBanner({ outcome, onDismiss }: { outcome: RefreshOutcome; onDismiss: () => void }) {
  const failed = outcome.results.filter((r) => r.error && !r.skipped)
  const skipped = outcome.results.filter((r) => r.skipped)
  const checked = outcome.results.length - skipped.length

  const allRecent = checked === 0 && skipped.length > 0

  return (
    <div className="banner" role="status">
      <div className="banner-main">
        <strong>
          {allRecent
            ? 'Everything was checked a moment ago'
            : outcome.new_jobs === 0
              ? 'No new jobs since the last check'
              : `${plural(outcome.new_jobs, 'new job')}, ${plural(outcome.new_matches, 'new match')}`}
        </strong>
        <span className="muted">
          {allRecent
            ? ` · all ${plural(skipped.length, 'source')} were polled in the last 5 minutes. Try again shortly.`
            : ` · checked ${plural(checked, 'source')}${skipped.length > 0 ? `, ${skipped.length} checked within the last 5 minutes` : ''}`}
        </span>
        <button className="btn" onClick={onDismiss} aria-label="Dismiss refresh summary">
          Dismiss
        </button>
      </div>
      {failed.length > 0 && (
        <details className="err">
          <summary>
            {plural(failed.length, 'source')} had problems
          </summary>
          <ul>
            {failed.map((r) => (
              <li key={r.source}>
                <strong>{sourceLabel(r.source)}</strong>: {r.error}
                {r.fetched > 0 && ` (still added ${plural(r.new_jobs, 'job')})`}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}
