import { fireEvent, render, screen } from '@testing-library/react'
import type { RefreshStatus } from '../api'
import { RefreshBanner } from './RefreshBanner'

const base: RefreshStatus = {
  running: false,
  started_at: '2026-11-16T10:00:00Z',
  finished_at: '2026-11-16T10:00:20Z',
  new_jobs: 0,
  new_matches: 0,
  results: [],
}

describe('RefreshBanner', () => {
  it('summarises new jobs and matches', () => {
    render(
      <RefreshBanner
        outcome={{ ...base, new_jobs: 12, new_matches: 3, results: [{ source: 'remoteok', fetched: 90, new_jobs: 12 }, { source: 'jobicy', fetched: 100, new_jobs: 0 }] }}
        onDismiss={() => {}}
      />,
    )
    expect(screen.getByRole('status')).toHaveTextContent('12 new jobs, 3 new matches')
    expect(screen.getByRole('status')).toHaveTextContent('checked 2 sources')
  })

  it('uses singular forms', () => {
    render(<RefreshBanner outcome={{ ...base, new_jobs: 1, new_matches: 1, results: [{ source: 'a', fetched: 1, new_jobs: 1 }] }} onDismiss={() => {}} />)
    expect(screen.getByRole('status')).toHaveTextContent('1 new job, 1 new match')
    expect(screen.getByRole('status')).toHaveTextContent('checked 1 source')
  })

  it('says plainly when nothing was new', () => {
    render(<RefreshBanner outcome={{ ...base, results: [{ source: 'a', fetched: 5, new_jobs: 0 }] }} onDismiss={() => {}} />)
    expect(screen.getByRole('status')).toHaveTextContent('No new jobs since the last check')
  })

  it('explains skipped sources instead of counting them as checked', () => {
    render(
      <RefreshBanner
        outcome={{ ...base, results: [{ source: 'a', fetched: 5, new_jobs: 0 }, { source: 'lever', fetched: 0, new_jobs: 0, skipped: true }, { source: 'ashby', fetched: 0, new_jobs: 0, skipped: true }] }}
        onDismiss={() => {}}
      />,
    )
    expect(screen.getByRole('status')).toHaveTextContent('checked 1 source, 2 checked within the last 5 minutes')
  })

  it('says so when every source was too recent to poll again', () => {
    render(
      <RefreshBanner
        outcome={{ ...base, results: [{ source: 'a', fetched: 0, new_jobs: 0, skipped: true }, { source: 'b', fetched: 0, new_jobs: 0, skipped: true }] }}
        onDismiss={() => {}}
      />,
    )
    expect(screen.getByRole('status')).toHaveTextContent('Everything was checked a moment ago')
    expect(screen.getByRole('status')).toHaveTextContent('all 2 sources were polled in the last 5 minutes')
    expect(screen.getByRole('status')).not.toHaveTextContent('No new jobs')
  })

  it('lists sources that failed, with their errors', () => {
    render(
      <RefreshBanner
        outcome={{ ...base, results: [{ source: 'lever', fetched: 0, new_jobs: 0, error: 'GET x: 503' }, { source: 'remoteok', fetched: 9, new_jobs: 1 }] }}
        onDismiss={() => {}}
      />,
    )
    expect(screen.getByText('1 source had problems')).toBeInTheDocument()
    expect(screen.getByText(/GET x: 503/)).toBeInTheDocument()
    expect(screen.getByText('Lever')).toBeInTheDocument()
  })

  it('mentions jobs that were still added despite an error', () => {
    render(<RefreshBanner outcome={{ ...base, results: [{ source: 'greenhouse', fetched: 40, new_jobs: 7, error: 'board x: 404' }] }} onDismiss={() => {}} />)
    expect(screen.getByText(/still added 7 jobs/)).toBeInTheDocument()
  })

  it('can be dismissed', () => {
    const onDismiss = vi.fn()
    render(<RefreshBanner outcome={base} onDismiss={onDismiss} />)
    fireEvent.click(screen.getByRole('button', { name: /dismiss/i }))
    expect(onDismiss).toHaveBeenCalledTimes(1)
  })
})
