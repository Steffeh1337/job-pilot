import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { MemoryRouter } from 'react-router-dom'

export const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

export interface Call {
  method: string
  path: string
  body?: BodyInit | null
}

type Handler = (call: Call) => Response | Promise<Response>

/**
 * A fake backend for component tests. Routes are keyed "METHOD /path" (query
 * strings are ignored when matching); every request is recorded.
 */
export function fakeApi(routes: Record<string, Handler | unknown>) {
  const calls: Call[] = []
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost')
    const call: Call = { method: init?.method ?? 'GET', path: url.pathname + url.search, body: init?.body }
    calls.push(call)
    const handler = routes[`${call.method} ${url.pathname}`]
    if (handler === undefined) return json({ error: `no route for ${call.method} ${url.pathname}` }, 404)
    if (typeof handler === 'function') return (handler as Handler)(call)
    return json(handler)
  })
  vi.stubGlobal('fetch', fn)
  return { calls, fn, to: (method: string, path: string) => calls.filter((c) => c.method === method && c.path.startsWith(path)) }
}

export function renderApp(ui: ReactElement, route = '/') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 0 } } })
  return { client, ...render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter></QueryClientProvider>) }
}
