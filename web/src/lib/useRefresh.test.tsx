import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { RefreshStatus } from '../api'
import { RefreshButton } from '../components/RefreshButton'
import { useRefresh, type RefreshOutcome } from './useRefresh'

const idle: RefreshStatus = { running: false, started_at: null, finished_at: null, new_jobs: 0, new_matches: 0, results: [] }
const running: RefreshStatus = { ...idle, running: true, started_at: '2026-11-16T10:00:00Z' }
const finished: RefreshStatus = {
  running: false,
  started_at: '2026-11-16T10:00:00Z',
  finished_at: '2026-11-16T10:00:20Z',
  new_jobs: 5,
  new_matches: 2,
  results: [{ source: 'remoteok', fetched: 97, new_jobs: 5 }],
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

/**
 * A fake backend. GET reports `initial` until a POST arrives (or from the
 * start, if the refresh was already running), then `running` for a poll or
 * two, then `finished`.
 */
function fakeBackend({ initial = idle, runningPolls = 1, postStatus = 202 }: { initial?: RefreshStatus; runningPolls?: number; postStatus?: number } = {}) {
  let phase: 'initial' | 'running' | 'done' = initial.running ? 'running' : 'initial'
  let seenRunning = 0
  const calls: { method: string; path: string }[] = []
  const fn = vi.fn(async (path: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ method, path })
    if (method === 'POST') {
      if (postStatus !== 202) return json({ error: 'the poller is not running' }, postStatus)
      phase = 'running'
      return json(running, 202)
    }
    if (phase === 'running' && seenRunning++ >= runningPolls) phase = 'done'
    return json(phase === 'done' ? finished : phase === 'running' ? running : initial)
  })
  return { fn, calls }
}

function Harness({ onFinished }: { onFinished: (o: RefreshOutcome) => void }) {
  const r = useRefresh({ onFinished, pollMs: 10 })
  return <RefreshButton running={r.running} onClick={r.refresh} error={r.error} />
}

function setup(backend: ReturnType<typeof fakeBackend>) {
  vi.stubGlobal('fetch', backend.fn)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const onFinished = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <Harness onFinished={onFinished} />
    </QueryClientProvider>,
  )
  return { onFinished, invalidate }
}

afterEach(() => vi.unstubAllGlobals())

describe('useRefresh', () => {
  it('starts a refresh, waits for it, then reports what it found and reloads the data', async () => {
    const backend = fakeBackend()
    const { onFinished, invalidate } = setup(backend)

    const button = await screen.findByRole('button', { name: /refresh/i })
    expect(button).toBeEnabled()
    fireEvent.click(button)

    await waitFor(() => expect(screen.getByRole('button', { name: /refreshing/i })).toBeDisabled())
    await waitFor(() => expect(onFinished).toHaveBeenCalledTimes(1))

    expect(onFinished.mock.calls[0]![0]).toMatchObject({ new_jobs: 5, new_matches: 2 })
    expect(invalidate).toHaveBeenCalled()
    expect(await screen.findByRole('button', { name: /^↻?\s*refresh$/i })).toBeEnabled()
    expect(backend.calls.filter((c) => c.method === 'POST')).toEqual([{ method: 'POST', path: '/api/refresh' }])
  })

  it('does not report an outcome just because the page loaded', async () => {
    const backend = fakeBackend()
    const { onFinished } = setup(backend)
    await screen.findByRole('button', { name: /refresh/i })
    await new Promise((r) => setTimeout(r, 60))
    expect(onFinished).not.toHaveBeenCalled()
    expect(backend.calls.every((c) => c.method === 'GET')).toBe(true)
  })

  it('shows a refresh that was already running when the page opened', async () => {
    const { onFinished } = setup(fakeBackend({ initial: running }))
    await waitFor(() => expect(screen.getByRole('button', { name: /refreshing/i })).toBeDisabled())
    await waitFor(() => expect(onFinished).toHaveBeenCalledTimes(1))
  })

  it('does not report the same refresh twice', async () => {
    const { onFinished } = setup(fakeBackend({ initial: running }))
    await waitFor(() => expect(onFinished).toHaveBeenCalledTimes(1))
    await new Promise((r) => setTimeout(r, 60))
    expect(onFinished).toHaveBeenCalledTimes(1)
  })

  it('shows why a click failed and leaves the button usable', async () => {
    const { onFinished } = setup(fakeBackend({ postStatus: 503 }))
    fireEvent.click(await screen.findByRole('button', { name: /refresh/i }))
    expect(await screen.findByRole('alert')).toHaveTextContent('the poller is not running')
    expect(screen.getByRole('button', { name: /refresh/i })).toBeEnabled()
    expect(onFinished).not.toHaveBeenCalled()
  })
})
