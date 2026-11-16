import { fireEvent, screen, waitFor } from '@testing-library/react'
import type { Job } from '../api'
import { fakeApi, json, renderApp } from '../testing'
import { Jobs } from './Jobs'

const job = (id: number, title: string, fit?: Job['fit']): Job => ({
  id, source: 'remoteok', url: 'https://example.com/' + id, title, company: 'Acme', location: 'Remote', remote: true,
  b2b: false, region_verified: true, stage: 'senior', posted_at: '2026-11-20T10:00:00Z', first_seen_at: '2026-11-20T10:00:00Z', fit,
})

const goFit: Job['fit'] = { score: 90, band: 'strong', matched: ['Go'], missing: [] }
const profileView = { exists: true, headline: '', seniority: 'senior', years_experience: 5, skills: [], languages: [], summary: '', has_embedding: false }
const noProfile = { ...profileView, exists: false }

const idle = { running: false, started_at: null, finished_at: null, new_jobs: 0, new_matches: 0, results: [] }
const funnel = { days: 3650, since: '', rows: [], rejected: {} }

function setup(profile: unknown, jobs: Job[] = [job(1, 'Go engineer', goFit), job(2, 'Mystery role')]) {
  return fakeApi({
    'GET /api/profile': profile,
    'GET /api/jobs': () => json({ total: jobs.length, jobs }),
    'GET /api/funnel': funnel,
    'GET /api/refresh': idle,
  })
}

describe('Jobs page with a CV', () => {
  it('shows the fit on each job that has one', async () => {
    setup(profileView)
    renderApp(<Jobs />, '/jobs')
    expect(await screen.findByText('Go engineer')).toBeInTheDocument()
    const badge = screen.getByText('Strong').closest('.fit')!
    expect(badge).toHaveTextContent('90')
    // The job without a fit shows no badge at all.
    expect(screen.getAllByText(/^(Strong|Good|Fair|Weak)$/)).toHaveLength(1)
  })

  it('offers sorting by fit and a minimum fit, and sends them to the server', async () => {
    const api = setup(profileView)
    renderApp(<Jobs />, '/jobs')
    await screen.findByText('Go engineer')

    fireEvent.change(screen.getByLabelText('Sort by'), { target: { value: 'fit' } })
    await waitFor(() => expect(api.to('GET', '/api/jobs').some((c) => c.path.includes('sort=fit'))).toBe(true))

    fireEvent.change(screen.getByLabelText('Minimum fit'), { target: { value: '55' } })
    await waitFor(() => expect(api.to('GET', '/api/jobs').some((c) => c.path.includes('sort=fit') && c.path.includes('min_fit=55'))).toBe(true))
  })

  it('reads the sort and minimum fit from the URL', async () => {
    const api = setup(profileView)
    renderApp(<Jobs />, '/jobs?sort=fit&min_fit=75')
    await screen.findByText('Go engineer')
    expect(screen.getByLabelText('Sort by')).toHaveValue('fit')
    expect(screen.getByLabelText('Minimum fit')).toHaveValue('75')
    expect(api.to('GET', '/api/jobs').some((c) => c.path.includes('min_fit=75'))).toBe(true)
  })
})

describe('Jobs page without a CV', () => {
  it('hides the fit controls and invites an upload', async () => {
    setup(noProfile, [job(1, 'Go engineer')])
    renderApp(<Jobs />, '/jobs')
    await screen.findByText('Go engineer')
    expect(screen.queryByLabelText('Sort by')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Minimum fit')).not.toBeInTheDocument()
    expect(await screen.findByRole('link', { name: /upload your cv to rank by fit/i })).toHaveAttribute('href', '/profile')
  })

  it('does not flash the invitation while the profile is still loading', async () => {
    fakeApi({
      'GET /api/profile': () => new Promise<Response>(() => {}), // never answers
      'GET /api/jobs': () => json({ total: 1, jobs: [job(1, 'Go engineer')] }),
      'GET /api/funnel': funnel, 'GET /api/refresh': idle,
    })
    renderApp(<Jobs />, '/jobs')
    await screen.findByText('Go engineer')
    expect(screen.queryByRole('link', { name: /upload your cv/i })).not.toBeInTheDocument()
  })
})
