import type { UseQueryResult } from '@tanstack/react-query'
import type { ReactNode } from 'react'

/** Renders loading and error states for a query, and its data once there. */
export function Async<T>({
  query,
  children,
  skeletonRows = 4,
}: {
  query: UseQueryResult<T>
  children: (data: T) => ReactNode
  skeletonRows?: number
}) {
  if (query.isPending) {
    return (
      <div role="status" aria-label="Loading" style={{ display: 'grid', gap: 10 }}>
        {Array.from({ length: skeletonRows }, (_, i) => (
          <div key={i} className="skeleton" />
        ))}
      </div>
    )
  }
  if (query.isError) {
    return (
      <div className="state error" role="alert">
        <strong>Couldn’t load this</strong>
        {query.error.message}
        <div style={{ marginTop: 12 }}>
          <button className="btn" onClick={() => void query.refetch()}>
            Try again
          </button>
        </div>
        <p className="muted" style={{ fontSize: '0.85rem' }}>
          Is the backend running? Start it with <code>make dev</code>.
        </p>
      </div>
    )
  }
  return <>{children(query.data)}</>
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="state">
      <strong>{title}</strong>
      {children}
    </div>
  )
}
