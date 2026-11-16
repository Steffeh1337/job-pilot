import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type Job, type Status } from '../api'
import { Async, Empty } from '../components/Async'
import { FitBadge } from '../components/FitBadge'
import { JobDrawer } from '../components/JobDrawer'
import { RefreshBanner } from '../components/RefreshBanner'
import { RefreshButton } from '../components/RefreshButton'
import { MIN_FIT_OPTIONS } from '../lib/fit'
import { formatSalary, locationSummary, reasonLabel, relativeTime, sourceLabel } from '../lib/format'
import { paramsFromQuery, queryFromParams, PAGE_SIZE } from '../lib/query'
import { useRefresh } from '../lib/useRefresh'

const STATUS_LABELS: [Status, string][] = [
  ['match', 'Matches'],
  ['rejected', 'Dropped'],
  ['all', 'All'],
]

export function Jobs() {
  const [params, setParams] = useSearchParams()
  const q = queryFromParams(params)
  const openId = Number.parseInt(params.get('job') ?? '', 10) || null

  // Keep the search box responsive and write to the URL once typing pauses.
  const [search, setSearch] = useState(q.q ?? '')
  useEffect(() => setSearch(q.q ?? ''), [q.q])
  useEffect(() => {
    const t = setTimeout(() => {
      if (search.trim() === (q.q ?? '')) return
      update({ q: search.trim() || undefined, page: 1 })
    }, 300)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search])

  // After a refresh, show the newest jobs first and mark the ones it found.
  const refresh = useRefresh({ onFinished: () => update({}) })
  const outcome = refresh.outcome
  // first_seen_at has one-second resolution, so compare from the start of the second.
  const newSince = outcome?.started_at ? Math.floor(new Date(outcome.started_at).getTime() / 1000) * 1000 : null

  /** Applies filter changes to the URL, keeping an open job and resetting the page. */
  function update(next: Partial<typeof q>) {
    const merged = { ...q, page: 1, ...next }
    const p = paramsFromQuery(merged)
    const job = params.get('job')
    if (job) p.set('job', job)
    setParams(p)
  }

  const closeJob = useCallback(() => {
    setParams((prev) => {
      const p = new URLSearchParams(prev)
      p.delete('job')
      return p
    })
  }, [setParams])

  function openJob(id: number) {
    setParams((prev) => {
      const p = new URLSearchParams(prev)
      p.set('job', String(id))
      return p
    })
  }

  // The source and reason dropdowns are filled from the all-time funnel.
  const options = useQuery({ queryKey: ['funnel', 3650], queryFn: () => api.funnel(3650), staleTime: 60_000 })
  const sources = (options.data?.rows ?? []).filter((r) => r.source !== 'TOTAL').map((r) => r.source)
  const reasons = Object.keys(options.data?.rejected ?? {})

  // Fit sorting needs an uploaded CV.
  const profile = useQuery({ queryKey: ['profile'], queryFn: api.profile, staleTime: 30_000 })
  const hasProfile = profile.data?.exists === true

  const jobs = useQuery({
    queryKey: ['jobs', q.status, q.source, q.reason, q.b2b, q.unverified, q.q, q.days, q.sort, q.minFit, q.page],
    queryFn: () => api.jobs(q),
    placeholderData: (prev) => prev,
  })

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Jobs</h1>
          <p>
            Matches are jobs that passed every filter. Switch to <em>Dropped</em> to read what was filtered out and spot
            filters that are too strict.
          </p>
        </div>
        <RefreshButton running={refresh.running} onClick={refresh.refresh} error={refresh.error} />
      </div>

      {outcome && <RefreshBanner outcome={outcome} onDismiss={refresh.dismiss} />}

      <div className="filters">
        <div className="segmented" role="group" aria-label="Show">
          {STATUS_LABELS.map(([s, label]) => (
            <button
              key={s}
              aria-pressed={q.status === s}
              onClick={() => update({ status: s, reason: s === 'match' ? undefined : q.reason })}
            >
              {label}
            </button>
          ))}
        </div>
        <input
          type="search"
          placeholder="Search title or company"
          aria-label="Search title or company"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="Source"
          value={q.source ?? ''}
          onChange={(e) => update({ source: e.target.value || undefined })}
        >
          <option value="">All sources</option>
          {sources.map((s) => (
            <option key={s} value={s}>
              {sourceLabel(s)}
            </option>
          ))}
        </select>
        {q.status !== 'match' && (
          <select
            aria-label="Drop reason"
            value={q.reason ?? ''}
            onChange={(e) => update({ reason: e.target.value || undefined })}
          >
            <option value="">Any reason</option>
            {reasons.map((r) => (
              <option key={r} value={r}>
                {reasonLabel(r)}
              </option>
            ))}
          </select>
        )}
        <label className="check">
          <input type="checkbox" checked={q.b2b} onChange={(e) => update({ b2b: e.target.checked })} />
          B2B / contract
        </label>
        <label className="check">
          <input type="checkbox" checked={q.unverified} onChange={(e) => update({ unverified: e.target.checked })} />
          Region unverified
        </label>
        {hasProfile ? (
          <>
            <select aria-label="Sort by" value={q.sort} onChange={(e) => update({ sort: e.target.value === 'fit' ? 'fit' : 'new' })}>
              <option value="new">Newest first</option>
              <option value="fit">Best fit first</option>
            </select>
            <select aria-label="Minimum fit" value={q.minFit} onChange={(e) => update({ minFit: Number(e.target.value) })}>
              {MIN_FIT_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </>
        ) : (
          profile.isSuccess && (
            <Link className="btn" to="/profile">
              Upload your CV to rank by fit
            </Link>
          )
        )}
        {q.days && (
          <button className="btn" onClick={() => update({ days: undefined })} title="Remove the time window">
            {q.days >= 3650 ? 'All time' : `Last ${q.days} d`} ✕
          </button>
        )}
      </div>

      <section className="card">
        <Async query={jobs} skeletonRows={6}>
          {(data) => (
            <>
              {data.jobs.length === 0 ? (
                <Empty title="No jobs match these filters">Try clearing a filter or widening the time window.</Empty>
              ) : (
                <ul className="jobs">
                  {data.jobs.map((j) => (
                    <li key={j.id}>
                      <JobRow
                        job={j}
                        isNew={newSince !== null && new Date(j.first_seen_at).getTime() >= newSince}
                        onOpen={() => openJob(j.id)}
                      />
                    </li>
                  ))}
                </ul>
              )}
              <Pager total={data.total} page={q.page} onPage={(page) => update({ page })} />
            </>
          )}
        </Async>
      </section>

      {openId !== null && <JobDrawer id={openId} onClose={closeJob} />}
    </>
  )
}

function JobRow({ job, isNew, onOpen }: { job: Job; isNew: boolean; onOpen: () => void }) {
  const salary = formatSalary(job.salary_min, job.salary_max, job.currency)
  return (
    <button className="job-row" onClick={onOpen}>
      <span className="job-title">{job.title}</span>
      <span className="job-meta">
        <span>{job.company || 'Unknown company'}</span>
        <span>{locationSummary(job.location, job.regions)}</span>
        {salary && <span>{salary}</span>}
      </span>
      <span className="job-badges">
        {isNew && <span className="badge new">New</span>}
        <span className="badge">{sourceLabel(job.source)}</span>
        {job.b2b && <span className="badge b2b">B2B / contract</span>}
        {job.reject_reason ? (
          <span className="badge danger">{reasonLabel(job.reject_reason)}</span>
        ) : (
          !job.region_verified && <span className="badge warn">Region unverified</span>
        )}
      </span>
      <span className="job-side">
        {job.fit && <FitBadge fit={job.fit} />}
        <span>{relativeTime(job.first_seen_at)}</span>
      </span>
    </button>
  )
}

function Pager({ total, page, onPage }: { total: number; page: number; onPage: (page: number) => void }) {
  if (total === 0) return null
  const from = (page - 1) * PAGE_SIZE + 1
  const to = Math.min(page * PAGE_SIZE, total)
  const last = Math.ceil(total / PAGE_SIZE)
  return (
    <nav className="pager" aria-label="Pagination">
      <span className="muted num">
        {from.toLocaleString()}–{to.toLocaleString()} of {total.toLocaleString()}
      </span>
      <span style={{ display: 'flex', gap: 8 }}>
        <button className="btn" disabled={page <= 1} onClick={() => onPage(page - 1)}>
          ← Previous
        </button>
        <button className="btn" disabled={page >= last} onClick={() => onPage(page + 1)}>
          Next →
        </button>
      </span>
    </nav>
  )
}
