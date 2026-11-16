import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { api, type Job } from '../api'
import { formatSalary, locationSummary, reasonLabel, relativeTime, sourceLabel, REASON_HELP } from '../lib/format'
import { safeUrl } from '../lib/url'
import { ApplicationSection } from './ApplicationSection'
import { Async } from './Async'
import { FitSection } from './FitSection'

export function JobDrawer({ id, onClose }: { id: number; onClose: () => void }) {
  const query = useQuery({ queryKey: ['job', id], queryFn: () => api.job(id) })
  const closeRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    closeRef.current?.focus()
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <>
      <div className="backdrop" onClick={onClose} aria-hidden="true" />
      <aside className="drawer" role="dialog" aria-modal="true" aria-label="Job details">
        <header>
          <div>
            {query.data ? (
              <>
                <h2 style={{ fontSize: '1.15rem' }}>{query.data.title}</h2>
                <div className="muted">
                  {query.data.company || 'Unknown company'} · {sourceLabel(query.data.source)}
                </div>
              </>
            ) : (
              <h2>Job</h2>
            )}
          </div>
          <button ref={closeRef} className="btn" onClick={onClose} aria-label="Close details">
            Close
          </button>
        </header>
        <div className="drawer-body">
          <Async query={query}>{(job) => <Details job={job} />}</Async>
        </div>
      </aside>
    </>
  )
}

function Details({ job }: { job: Job }) {
  const link = safeUrl(job.url)
  const salary = formatSalary(job.salary_min, job.salary_max, job.currency)
  return (
    <>
      <div className="job-badges" style={{ marginBottom: 14 }}>
        {job.b2b && <span className="badge b2b">B2B / contract</span>}
        {job.reject_reason ? (
          <span className="badge danger" title={REASON_HELP[job.reject_reason]}>
            Dropped: {reasonLabel(job.reject_reason)}
          </span>
        ) : (
          <span className="badge ok">Match</span>
        )}
        {!job.reject_reason && !job.region_verified && <span className="badge warn">Region unverified</span>}
      </div>

      {!job.reject_reason && !job.region_verified && (
        <p className="note">
          The source gave no location for this job, so it was only <em>not ruled out</em> for someone in Romania. Read
          the description for restrictions before applying.
        </p>
      )}

      <dl className="facts">
        <dt>Open to</dt>
        <dd>{locationSummary(job.location, job.regions)}</dd>
        {job.employment_type && (
          <>
            <dt>Type</dt>
            <dd>{job.employment_type}</dd>
          </>
        )}
        {salary && (
          <>
            <dt>Salary</dt>
            <dd>{salary}</dd>
          </>
        )}
        <dt>Posted</dt>
        <dd>{relativeTime(job.posted_at)}</dd>
        <dt>First seen</dt>
        <dd>{relativeTime(job.first_seen_at)}</dd>
        {job.tags && job.tags.length > 0 && (
          <>
            <dt>Tags</dt>
            <dd>{job.tags.join(', ')}</dd>
          </>
        )}
      </dl>

      {link && (
        <p>
          <a className="btn primary" href={link} target="_blank" rel="noopener noreferrer">
            Open posting ↗
          </a>
        </p>
      )}

      <ApplicationSection job={job} />

      {job.fit && <FitSection fit={job.fit} />}

      <h3 style={{ margin: '18px 0 8px' }}>Description</h3>
      {job.description ? (
        <div className="description">{job.description}</div>
      ) : (
        <p className="muted">No description was provided by the source.</p>
      )}
    </>
  )
}
