import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, type ApplicationStatus, type Job } from '../api'
import { applicationDirty, APPLICATION_STATUSES, statusLabel, statusTone, today } from '../lib/applications'

/**
 * Tracks whether you applied. Applying happens on the company's own site, so
 * this is a note to yourself: nothing is sent anywhere.
 */
export function ApplicationSection({ job }: { job: Job }) {
  const qc = useQueryClient()
  const changed = () => {
    void qc.invalidateQueries({ queryKey: ['job', job.id] })
    void qc.invalidateQueries({ queryKey: ['jobs'] })
  }
  const save = useMutation({
    mutationFn: (a: { status: ApplicationStatus; applied_on: string; notes: string }) => api.saveApplication(job.id, a),
    onSuccess: changed,
  })
  const remove = useMutation({ mutationFn: () => api.removeApplication(job.id), onSuccess: changed })
  const app = job.application

  if (!app) {
    return (
      <section className="application" aria-labelledby="application-h">
        <div className="fit-head">
          <h3 id="application-h">Application</h3>
        </div>
        <p className="muted small">Applied on the company’s site? Record it here to keep track. Nothing is sent anywhere.</p>
        <button
          className="btn primary"
          disabled={save.isPending}
          onClick={() => save.mutate({ status: 'applied', applied_on: today(), notes: '' })}
        >
          {save.isPending ? 'Saving…' : 'Mark as applied'}
        </button>
        {save.isError && (
          <p className="refresh-error" role="alert">
            {save.error.message}
          </p>
        )}
      </section>
    )
  }

  return <Tracked key={app.updated_at} job={job} save={save} removing={remove.isPending} onRemove={() => remove.mutate()} error={save.error ?? remove.error} />
}

function Tracked({
  job,
  save,
  removing,
  onRemove,
  error,
}: {
  job: Job
  save: { mutate: (a: { status: ApplicationStatus; applied_on: string; notes: string }) => void; isPending: boolean }
  removing: boolean
  onRemove: () => void
  error: Error | null
}) {
  const app = job.application!
  const [form, setForm] = useState({ status: app.status, applied_on: app.applied_on, notes: app.notes })
  const dirty = applicationDirty(form, app)

  return (
    <section className="application" aria-labelledby="application-h">
      <div className="fit-head">
        <h3 id="application-h">Application</h3>
        <span className={`badge ${statusTone(app.status)}`}>{statusLabel(app.status)}</span>
      </div>
      <form
        className="profile-form"
        onSubmit={(e) => {
          e.preventDefault()
          if (dirty) save.mutate(form)
        }}
      >
        <div className="row">
          <label>
            <span>Status</span>
            <select value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value as ApplicationStatus })}>
              {APPLICATION_STATUSES.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>Applied on</span>
            <input
              type="date"
              value={form.applied_on}
              max={today()}
              required
              onChange={(e) => setForm({ ...form, applied_on: e.target.value })}
            />
          </label>
        </div>
        <label>
          <span>Notes</span>
          <textarea
            rows={3}
            maxLength={2000}
            value={form.notes}
            placeholder="Where you applied, who you spoke to, next steps…"
            onChange={(e) => setForm({ ...form, notes: e.target.value })}
          />
        </label>
        {error && (
          <p className="refresh-error" role="alert">
            {error.message}
          </p>
        )}
        <div className="form-actions">
          <button type="submit" className="btn primary" disabled={!dirty || save.isPending || !form.applied_on}>
            {save.isPending ? 'Saving…' : 'Save'}
          </button>
          <span className="spacer" />
          <button
            type="button"
            className="btn danger"
            disabled={removing}
            onClick={() => {
              if (window.confirm('Remove this application record? The job stays in your list.')) onRemove()
            }}
          >
            Remove
          </button>
        </div>
      </form>
    </section>
  )
}
