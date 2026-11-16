import { fireEvent, screen, waitFor } from '@testing-library/react'
import type { Application, Job } from '../api'
import { fakeApi, json, renderApp } from '../testing'
import { Jobs } from './Jobs'

const applied: Application = { status: 'interviewing', applied_on: '2026-11-12', notes: 'Call on Monday', updated_at: '2026-11-13T09:00:00Z' }

const job = (id: number, title: string, application?: Application): Job => ({
  id, source: 'remoteok', url: 'https://example.com/' + id, title, company: 'Acme', location: 'Remote', remote: true,
  b2b: false, region_verified: true, stage: 'senior', posted_at: '2026-11-20T10:00:00Z', first_seen_at: '2026-11-20T10:00:00Z',
  description: 'Build things.', application,
})

const idle = { running: false, started_at: null, finished_at: null, new_jobs: 0, new_matches: 0, results: [] }
const noProfile = { exists: false, headline: '', seniority: '', years_experience: 0, skills: [], languages: [], summary: '', highlights: [], has_embedding: false }

function setup(extra: Record<string, unknown> = {}) {
  const jobs = [job(1, 'Go engineer', applied), job(2, 'Rust engineer')]
  return fakeApi({
    'GET /api/profile': noProfile,
    'GET /api/jobs': () => json({ total: jobs.length, jobs }),
    'GET /api/funnel': { days: 3650, since: '', rows: [], rejected: {} },
    'GET /api/refresh': idle,
    ...extra,
  })
}

describe('Jobs page: applications', () => {
  it('shows where you stand on a job you applied to, and nothing on the others', async () => {
    setup()
    renderApp(<Jobs />, '/jobs')
    expect(await screen.findByText('Interviewing · applied 12 Nov 2026')).toBeInTheDocument()
    expect(screen.getAllByText(/applied 12 Nov/)).toHaveLength(1)
  })

  it('has an Applied view that asks for everything you applied to', async () => {
    const api = setup()
    renderApp(<Jobs />, '/jobs')
    await screen.findByText('Go engineer')
    fireEvent.click(screen.getByRole('button', { name: 'Applied' }))
    await waitFor(() => expect(api.to('GET', '/api/jobs').some((c) => c.path.includes('applied=1') && c.path.includes('status=all'))).toBe(true))
    expect(screen.getByRole('button', { name: 'Applied' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Matches' })).toHaveAttribute('aria-pressed', 'false')
    // Hiding applied jobs makes no sense inside the tracker.
    expect(screen.queryByLabelText('Hide applied')).not.toBeInTheDocument()
  })

  it('can hide what you already applied to', async () => {
    const api = setup()
    renderApp(<Jobs />, '/jobs')
    fireEvent.click(await screen.findByLabelText('Hide applied'))
    await waitFor(() => expect(api.to('GET', '/api/jobs').some((c) => c.path.includes('applied=0'))).toBe(true))
  })

  it('says so when nothing is tracked yet', async () => {
    setup({ 'GET /api/jobs': () => json({ total: 0, jobs: [] }) })
    renderApp(<Jobs />, '/jobs?status=all&applied=1')
    expect(await screen.findByText('Nothing tracked yet')).toBeInTheDocument()
  })
})

describe('Job drawer: application', () => {
  it('marks a job as applied today and refreshes the list', async () => {
    const saved: Application = { status: 'applied', applied_on: '2026-11-20', notes: '', updated_at: '2026-11-20T12:00:00Z' }
    let current: Job = job(2, 'Rust engineer')
    const api = setup({
      'GET /api/jobs/2': () => json(current),
      'PUT /api/jobs/2/application': () => {
        current = { ...current, application: saved }
        return json(saved)
      },
    })
    renderApp(<Jobs />, '/jobs?job=2')
    fireEvent.click(await screen.findByRole('button', { name: 'Mark as applied' }))

    await waitFor(() => expect(api.to('PUT', '/api/jobs/2/application')).toHaveLength(1))
    const body = JSON.parse(String(api.to('PUT', '/api/jobs/2/application')[0]!.body))
    expect(body).toMatchObject({ status: 'applied', notes: '' })
    expect(body.applied_on).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    // The drawer now shows the record, and the list was asked again.
    expect(await screen.findByLabelText('Notes')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mark as applied' })).not.toBeInTheDocument()
    expect(api.to('GET', '/api/jobs').length).toBeGreaterThan(1)
  })

  it('edits the stage and notes, and only saves when something changed', async () => {
    const api = setup({
      'GET /api/jobs/1': () => json(job(1, 'Go engineer', applied)),
      'PUT /api/jobs/1/application': (call: { body?: BodyInit | null }) => json({ ...applied, ...JSON.parse(String(call.body)) }),
    })
    renderApp(<Jobs />, '/jobs?job=1')
    const save = await screen.findByRole('button', { name: 'Save' })
    expect(save).toBeDisabled()
    expect(screen.getByLabelText('Notes')).toHaveValue('Call on Monday')

    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'offer' } })
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'Offer received' } })
    fireEvent.click(save)

    await waitFor(() => expect(api.to('PUT', '/api/jobs/1/application')).toHaveLength(1))
    expect(JSON.parse(String(api.to('PUT', '/api/jobs/1/application')[0]!.body))).toEqual({
      status: 'offer', applied_on: '2026-11-12', notes: 'Offer received',
    })
  })

  it('shows the server’s reason when saving fails', async () => {
    setup({
      'GET /api/jobs/1': () => json(job(1, 'Go engineer', applied)),
      'PUT /api/jobs/1/application': () => json({ error: 'applied_on can’t be in the future' }, 400),
    })
    renderApp(<Jobs />, '/jobs?job=1')
    fireEvent.change(await screen.findByLabelText('Notes'), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('in the future')
  })

  it('removes the record after confirmation, and not when you cancel', async () => {
    const api = setup({
      'GET /api/jobs/1': () => json(job(1, 'Go engineer', applied)),
      'DELETE /api/jobs/1/application': () => new Response(null, { status: 204 }),
    })
    renderApp(<Jobs />, '/jobs?job=1')
    const remove = await screen.findByRole('button', { name: 'Remove' })

    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    fireEvent.click(remove)
    expect(api.to('DELETE', '/api/jobs/1/application')).toHaveLength(0)

    confirm.mockReturnValue(true)
    fireEvent.click(remove)
    await waitFor(() => expect(api.to('DELETE', '/api/jobs/1/application')).toHaveLength(1))
  })
})
