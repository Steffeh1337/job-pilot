import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api'

export const MAX_HIGHLIGHTS = 3

/**
 * Up to three bullets that show exceptional ability. They can be written by
 * hand or drafted from the CV by the local model; a draft only fills the form,
 * and nothing is stored until the profile is saved.
 */
export function HighlightsEditor({ highlights, onChange }: { highlights: string[]; onChange: (h: string[]) => void }) {
  const [warnings, setWarnings] = useState<string[]>([])
  const draft = useMutation({
    mutationFn: api.suggestHighlights,
    onSuccess: (res) => {
      setWarnings(res.warnings)
      if (res.highlights.length > 0) onChange(res.highlights)
    },
  })

  function suggest() {
    const written = highlights.some((h) => h.trim())
    if (written && !window.confirm('Drafting replaces the highlights you have written (until you discard). Continue?')) return
    setWarnings([])
    draft.mutate()
  }

  const set = (i: number, text: string) => onChange(highlights.map((h, j) => (j === i ? text : h)))

  return (
    <div className="highlights">
      {highlights.map((h, i) => (
        <div className="highlight" key={i}>
          <textarea
            rows={2}
            maxLength={280}
            value={h}
            aria-label={`Highlight ${i + 1}`}
            placeholder="e.g. Led the migration of 40 services to Kubernetes, cutting deploy time by 70%"
            onChange={(e) => set(i, e.target.value)}
          />
          <button type="button" className="chip-x" aria-label={`Remove highlight ${i + 1}`} onClick={() => onChange(highlights.filter((_, j) => j !== i))}>
            ×
          </button>
        </div>
      ))}
      <div className="highlight-actions">
        {highlights.length < MAX_HIGHLIGHTS && (
          <button type="button" className="btn" onClick={() => onChange([...highlights, ''])}>
            Add a highlight
          </button>
        )}
        <button type="button" className="btn" disabled={draft.isPending} onClick={suggest}>
          {draft.isPending ? 'Drafting…' : 'Draft from my CV'}
        </button>
      </div>
      <p className="muted small">
        The local model picks the achievements your CV backs up best, and drops anything it can’t trace to the CV. It stays on this
        computer. Check each one is true, then save.
      </p>
      {draft.isPending && (
        <p className="muted small" role="status">
          Reading your CV with the local model. This can take a minute.
        </p>
      )}
      {warnings.map((w) => (
        <p key={w} className="note small" role="status">
          {w}
        </p>
      ))}
      {draft.isError && (
        <p className="refresh-error" role="alert">
          {draft.error.message}
        </p>
      )}
    </div>
  )
}
