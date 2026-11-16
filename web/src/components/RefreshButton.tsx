export function RefreshButton({
  running,
  onClick,
  error,
}: {
  running: boolean
  onClick: () => void
  error?: Error | null
}) {
  return (
    <div className="refresh">
      <button className="btn primary" onClick={onClick} disabled={running} aria-busy={running}>
        <span className={running ? 'spinner' : undefined} aria-hidden="true">
          {running ? '' : '↻'}
        </span>
        {running ? 'Refreshing…' : 'Refresh'}
      </button>
      <span className="muted refresh-note" role="status">
        {running && 'Checking every source. This can take up to a minute.'}
      </span>
      {error && !running && (
        <span className="refresh-error" role="alert">
          Couldn’t refresh: {error.message}
        </span>
      )}
    </div>
  )
}
