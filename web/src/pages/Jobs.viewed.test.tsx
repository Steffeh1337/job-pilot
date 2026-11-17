import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import type { Job } from '../api'
import { fakeApi, json, renderApp } from '../testing'
import { Jobs } from './Jobs'

const job = (id: number, title: string, viewed = false): Job => ({
  id, source: 'remoteok', url: 'https://example.com/' + id, title, company: 'Acme', location: 'Remote', remote: true,
  b2b: false, viewed, region_verified: true, stage: 'senior', posted_at: '2026-11-20T10:00:00Z', first_seen_at: '2026-11-20T10:00:00Z',
  description: 'Build things.',
})

const idle = { running: false, started_at: null, finished_at: null, new_jobs: 0, new_matches: 0, results: [] }
const noProfile = { exists: false, headline: '', seniority: '', years_experience: 0, skills: [], languages: [], summary: '', highlights: [], has_embedding: false }
const noContent = () => new Response(null, { status: 204 })

function setup(jobs: Job[], extra: Record<string, unknown> = {}) {
  return fakeApi({
    'GET /api/profile': noProfile,
    'GET /api/jobs': () => json({ total: jobs.length, jobs }),
    'GET /api/funnel': { days: 3650, since: '', rows: [], rejected: {} },
    'GET /api/refresh': idle,
    ...extra,
  })
}

const rowOf = (title: string) => screen.getByText(title).closest('.job-item') as HTMLElement

describe('Jobs page: viewed jobs', () => {
  it('greys out viewed jobs, labels them and offers to mark them unread', async () => {
    setup([job(1, 'Go engineer'), job(2, 'Rust engineer', true)])
    renderApp(<Jobs />, '/jobs')
    await screen.findByText('Go engineer')

    expect(rowOf('Rust engineer')).toHaveClass('viewed')
    expect(within(rowOf('Rust engineer')).getByText('Viewed')).toBeInTheDocument()
    expect(within(rowOf('Rust engineer')).getByRole('button', { name: 'Mark Rust engineer as unread' })).toBeInTheDocument()

    expect(rowOf('Go engineer')).not.toHaveClass('viewed')
    expect(within(rowOf('Go engineer')).queryByText('Viewed')).not.toBeInTheDocument()
    expect(within(rowOf('Go engineer')).queryByRole('button', { name: /unread/ })).not.toBeInTheDocument()
  })

  it('marks a job unread from the list and asks for the list again', async () => {
    const api = setup([job(2, 'Rust engineer', true)], { 'DELETE /api/jobs/2/viewed': noContent })
    renderApp(<Jobs />, '/jobs')
    fireEvent.click(await screen.findByRole('button', { name: 'Mark Rust engineer as unread' }))

    await waitFor(() => expect(api.to('DELETE', '/api/jobs/2/viewed')).toHaveLength(1))
    await waitFor(() => expect(api.to('GET', '/api/jobs').length).toBeGreaterThan(1))
  })

  it('does not grey out the tracker', async () => {
    setup([job(2, 'Rust engineer', true)])
    renderApp(<Jobs />, '/jobs?status=all&applied=1')
    await screen.findByText('Rust engineer')
    expect(rowOf('Rust engineer')).not.toHaveClass('viewed')
  })
})

describe('Job drawer: viewed', () => {
  it('counts as viewing the job, and can be undone', async () => {
    let current = job(2, 'Rust engineer')
    const api = setup([current], {
      'GET /api/jobs/2': () => json(current),
      'PUT /api/jobs/2/viewed': () => {
        current = { ...current, viewed: true }
        return noContent()
      },
      'DELETE /api/jobs/2/viewed': () => {
        current = { ...current, viewed: false }
        return noContent()
      },
    })
    renderApp(<Jobs />, '/jobs?job=2')

    await waitFor(() => expect(api.to('PUT', '/api/jobs/2/viewed')).toHaveLength(1))
    const dialog = await screen.findByRole('dialog')
    const unread = await within(dialog).findByRole('button', { name: 'Mark as unread' })
    fireEvent.click(unread)

    await waitFor(() => expect(api.to('DELETE', '/api/jobs/2/viewed')).toHaveLength(1))
    fireEvent.click(await within(dialog).findByRole('button', { name: 'Mark as viewed' }))
    await waitFor(() => expect(api.to('PUT', '/api/jobs/2/viewed')).toHaveLength(2))
    expect(await within(dialog).findByRole('button', { name: 'Mark as unread' })).toBeInTheDocument()
  })

  it('still shows the job when the viewed call fails', async () => {
    setup([job(2, 'Rust engineer')], {
      'GET /api/jobs/2': () => json(job(2, 'Rust engineer')),
      'PUT /api/jobs/2/viewed': () => json({ error: 'database is locked' }, 500),
    })
    renderApp(<Jobs />, '/jobs?job=2')
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('Build things.')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Mark as viewed' })).toBeInTheDocument()
  })
})
