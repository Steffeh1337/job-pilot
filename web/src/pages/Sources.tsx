import { useQuery } from '@tanstack/react-query'
import { api, type Poll } from '../api'
import { Async, Empty } from '../components/Async'
import { formatDuration, formatInterval, nextDue, relativeTime, sourceLabel } from '../lib/format'

export function Sources() {
  const polls = useQuery({ queryKey: ['polls'], queryFn: api.polls, refetchInterval: 30_000 })
  return (
    <>
      <div className="page-head">
        <div>
          <h1>Sources</h1>
          <p>
            The scheduler wakes every tick, but each source is only polled once its own interval has passed, because
            most boards refresh hourly or slower.
          </p>
        </div>
      </div>
      <section className="card">
        <Async query={polls} skeletonRows={6}>
          {(data) =>
            data.length === 0 ? (
              <Empty title="No sources are running">
                Start the poller with <code>make dev</code>.
              </Empty>
            ) : (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Source</th>
                      <th className="left">Status</th>
                      <th>Last poll</th>
                      <th>Next</th>
                      <th>Every</th>
                      <th>Fetched</th>
                      <th>New</th>
                      <th>Took</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.map((p) => (
                      <PollRow key={p.source} poll={p} />
                    ))}
                  </tbody>
                </table>
              </div>
            )
          }
        </Async>
      </section>
    </>
  )
}

function PollRow({ poll }: { poll: Poll }) {
  const never = poll.started_at === null
  return (
    <tr className={never ? 'dim' : undefined}>
      <td>{sourceLabel(poll.source)}</td>
      <td className="left">
        {never ? (
          <span className="badge">Not polled yet</span>
        ) : poll.error ? (
          <details className="err">
            <summary>
              <span className="badge danger">{poll.fetched > 0 ? 'Partial' : 'Failed'}</span>
            </summary>
            <pre>{poll.error}</pre>
          </details>
        ) : (
          <span className="badge ok">OK</span>
        )}
      </td>
      <td className="num">{relativeTime(poll.started_at)}</td>
      <td className="num">{nextDue(poll.started_at, poll.interval_seconds)}</td>
      <td className="num">{formatInterval(poll.interval_seconds)}</td>
      <td className="num">{never ? '—' : poll.fetched.toLocaleString()}</td>
      <td className="num">{never ? '—' : poll.new_jobs.toLocaleString()}</td>
      <td className="num">{never ? '—' : formatDuration(poll.duration_ms)}</td>
    </tr>
  )
}
