import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type FunnelResponse } from '../api'
import { Async, Empty } from '../components/Async'
import { FunnelChart } from '../components/FunnelChart'
import { formatPct, pct, sourceRows, totalRow } from '../lib/funnel'
import { REASON_HELP, reasonLabel, sourceLabel } from '../lib/format'

const WINDOWS = [
  { days: 1, label: '24 h' },
  { days: 7, label: '7 days' },
  { days: 30, label: '30 days' },
  { days: 3650, label: 'All time' },
] as const

function parseDays(value: string | null): number {
  const n = Number.parseInt(value ?? '', 10)
  return WINDOWS.some((w) => w.days === n) ? n : 7
}

export function Overview() {
  const [params, setParams] = useSearchParams()
  const days = parseDays(params.get('days'))
  const windowLabel = WINDOWS.find((w) => w.days === days)!.label.toLowerCase()

  const funnel = useQuery({ queryKey: ['funnel', days], queryFn: () => api.funnel(days), refetchInterval: 60_000 })
  const unverified = useQuery({
    queryKey: ['unverified', days],
    queryFn: () => api.jobs({ status: 'match', unverified: true, days, limit: 1 }),
    refetchInterval: 60_000,
  })

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Funnel</h1>
          <p>
            Distinct jobs first seen in the window, and how many survive each filter. Use it to see which sources are
            worth keeping and which filter is dropping too much.
          </p>
        </div>
        <div className="segmented" role="group" aria-label="Time window">
          {WINDOWS.map((w) => (
            <button
              key={w.days}
              aria-pressed={w.days === days}
              onClick={() => setParams(w.days === 7 ? {} : { days: String(w.days) })}
            >
              {w.label}
            </button>
          ))}
        </div>
      </div>

      <Async query={funnel} skeletonRows={8}>
        {(data) => {
          const total = totalRow(data.rows)
          if (total.new === 0) return <NothingYet windowLabel={windowLabel} />
          return (
            <>
              <div className="grid stats">
                <Link className="stat" to={`/jobs?days=${days}`}>
                  <div className="label">Matches</div>
                  <div className="value num">{total.senior.toLocaleString()}</div>
                  <div className="hint">
                    of {total.new.toLocaleString()} seen ({formatPct(pct(total.senior, total.new))})
                  </div>
                </Link>
                <Link className="stat" to={`/jobs?b2b=1&days=${days}`}>
                  <div className="label">Mention B2B / contract</div>
                  <div className="value num">{total.b2b.toLocaleString()}</div>
                  <div className="hint">among the matches</div>
                </Link>
                <Link
                  className={`stat ${(unverified.data?.total ?? 0) > 0 ? 'attention' : ''}`}
                  to={`/jobs?unverified=1&days=${days}`}
                >
                  <div className="label">Need a region check</div>
                  <div className="value num">{unverified.data ? unverified.data.total.toLocaleString() : '…'}</div>
                  <div className="hint">source gave no location</div>
                </Link>
              </div>

              <section className="card" aria-labelledby="funnel-h">
                <div className="card-head">
                  <h2 id="funnel-h">All sources, {windowLabel}</h2>
                  <p>Percentages are the share of the previous step that survived.</p>
                </div>
                <FunnelChart row={total} />
              </section>

              <section className="card" aria-labelledby="sources-h">
                <div className="card-head">
                  <h2 id="sources-h">By source</h2>
                  <p>Yield is matches as a share of everything the source returned.</p>
                </div>
                <SourceTable data={data} days={days} />
              </section>

              <section className="card" aria-labelledby="why-h">
                <div className="card-head">
                  <h2 id="why-h">Why jobs were dropped</h2>
                  <p>Open a reason to read the dropped jobs and check the filter is right.</p>
                </div>
                <Reasons data={data} days={days} />
              </section>
            </>
          )
        }}
      </Async>
    </>
  )
}

function NothingYet({ windowLabel }: { windowLabel: string }) {
  return (
    <div className="card">
      <Empty title={`No jobs seen in the last ${windowLabel}`}>
        Start collecting with <code>make dev</code> (or run one pass with <code>make once</code>), then refresh.
      </Empty>
    </div>
  )
}

function SourceTable({ data, days }: { data: FunnelResponse; days: number }) {
  const rows = sourceRows(data.rows)
  const total = totalRow(data.rows)
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Source</th>
            <th>Seen</th>
            <th>Unique</th>
            <th>Region</th>
            <th>Role</th>
            <th>Stack</th>
            <th>Match</th>
            <th>B2B</th>
            <th>Yield</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.source} className={r.senior === 0 ? 'dim' : undefined}>
              <td>
                <Link to={`/jobs?source=${r.source}&days=${days}`}>{sourceLabel(r.source)}</Link>
              </td>
              <td className="num">{r.new.toLocaleString()}</td>
              <td className="num">{r.unique.toLocaleString()}</td>
              <td className="num">{r.region.toLocaleString()}</td>
              <td className="num">{r.role.toLocaleString()}</td>
              <td className="num">{r.stack.toLocaleString()}</td>
              <td className="num">
                <strong>{r.senior.toLocaleString()}</strong>
              </td>
              <td className="num">{r.b2b.toLocaleString()}</td>
              <td className="num">
                <span className="yield">{formatPct(pct(r.senior, r.new))}</span>
              </td>
            </tr>
          ))}
        </tbody>
        <tfoot>
          <tr>
            <td>Total</td>
            <td className="num">{total.new.toLocaleString()}</td>
            <td className="num">{total.unique.toLocaleString()}</td>
            <td className="num">{total.region.toLocaleString()}</td>
            <td className="num">{total.role.toLocaleString()}</td>
            <td className="num">{total.stack.toLocaleString()}</td>
            <td className="num">{total.senior.toLocaleString()}</td>
            <td className="num">{total.b2b.toLocaleString()}</td>
            <td className="num">{formatPct(pct(total.senior, total.new))}</td>
          </tr>
        </tfoot>
      </table>
    </div>
  )
}

function Reasons({ data, days }: { data: FunnelResponse; days: number }) {
  const entries = Object.entries(data.rejected).sort((a, b) => b[1] - a[1])
  if (entries.length === 0) return <Empty title="Nothing dropped yet" />
  const max = entries[0]![1]
  return (
    <ul className="reasons">
      {entries.map(([reason, count]) => (
        <li key={reason}>
          <Link to={`/jobs?status=rejected&reason=${encodeURIComponent(reason)}&days=${days}`}>
            <span>{reasonLabel(reason)}</span>
            <span className="track" aria-hidden="true">
              <span className="bar" style={{ display: 'block', width: `${pct(count, max)}%` }} />
            </span>
            <span className="count num">{count.toLocaleString()}</span>
            {REASON_HELP[reason] && <span className="help">{REASON_HELP[reason]}</span>}
          </Link>
        </li>
      ))}
    </ul>
  )
}
