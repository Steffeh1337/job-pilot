import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { api, type Profile as ProfileData, type ProfileResponse, type ProfileView } from '../api'
import { Async } from '../components/Async'
import { MatchStatus } from '../components/MatchStatus'
import { SkillsEditor } from '../components/SkillsEditor'
import { relativeTime } from '../lib/format'
import { extractedByLabel, isDirty, parseList, SENIORITIES, toDraft } from '../lib/profile'

export function Profile() {
  const qc = useQueryClient()
  const profile = useQuery({ queryKey: ['profile'], queryFn: api.profile })
  const status = useQuery({
    queryKey: ['fit-status'],
    queryFn: api.fitStatus,
    refetchInterval: (q) => (q.state.data?.index.syncing ? 2000 : 20_000),
  })
  const [warnings, setWarnings] = useState<string[]>([])

  const sync = useMutation({
    mutationFn: api.fitSync,
    onSuccess: (data) => qc.setQueryData(['fit-status'], data),
  })

  /** After the profile changes, fit scores and the index status are out of date. */
  function profileChanged(view: ProfileView) {
    qc.setQueryData(['profile'], view)
    void qc.invalidateQueries({ queryKey: ['fit-status'] })
    void qc.invalidateQueries({ queryKey: ['jobs'] })
  }

  function received(res: ProfileResponse) {
    setWarnings(res.warnings)
    profileChanged(res.profile)
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Profile</h1>
          <p>
            Upload your CV to rank jobs by how well they fit you. It is read here, on this computer, and you can correct
            anything it gets wrong.
          </p>
        </div>
      </div>

      <Async query={status} skeletonRows={4}>
        {(s) => <MatchStatus status={s} onSync={() => sync.mutate()} syncing={sync.isPending} />}
      </Async>

      <Async query={profile} skeletonRows={5}>
        {(p) => (
          <>
            <UploadCard exists={p.exists} onReceived={received} onStart={() => setWarnings([])} />
            {warnings.length > 0 && (
              <div className="note" role="status">
                {warnings.map((w) => (
                  <p key={w} style={{ margin: 0 }}>
                    {w}
                  </p>
                ))}
              </div>
            )}
            {p.exists && (
              <ProfileEditor
                key={`${p.updated_at}`}
                profile={p}
                onSaved={profileChanged}
                onRead={received}
                onDeleted={() => {
                  setWarnings([])
                  qc.setQueryData(['profile'], { ...emptyProfile, exists: false })
                  void qc.invalidateQueries({ queryKey: ['fit-status'] })
                  void qc.invalidateQueries({ queryKey: ['jobs'] })
                }}
              />
            )}
          </>
        )}
      </Async>
    </>
  )
}

const emptyProfile: ProfileView = {
  exists: false, headline: '', seniority: '', years_experience: 0, skills: [], languages: [], summary: '', has_embedding: false,
}

function UploadCard({ exists, onReceived, onStart }: { exists: boolean; onReceived: (r: ProfileResponse) => void; onStart: () => void }) {
  const input = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)
  const upload = useMutation({ mutationFn: api.uploadCV, onSuccess: onReceived })

  function pick(file: File | undefined) {
    if (!file) return
    onStart()
    upload.mutate(file)
  }

  return (
    <section className="card" aria-labelledby="upload-h">
      <div className="card-head">
        <h2 id="upload-h">{exists ? 'Replace your CV' : 'Upload your CV'}</h2>
        <p>PDF, DOCX or plain text, up to 8 MB. Scanned images can’t be read.</p>
      </div>
      <div
        className={`dropzone${dragging ? ' dragging' : ''}`}
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragging(false)
          pick(e.dataTransfer.files[0])
        }}
      >
        <input
          ref={input}
          type="file"
          accept=".pdf,.docx,.txt,.md,application/pdf,application/vnd.openxmlformats-officedocument.wordprocessingml.document,text/plain"
          aria-label="Choose your CV"
          hidden
          onChange={(e) => {
            pick(e.target.files?.[0])
            e.target.value = '' // allow choosing the same file again
          }}
        />
        <button className="btn primary" onClick={() => input.current?.click()} disabled={upload.isPending}>
          {upload.isPending ? 'Reading your CV…' : 'Choose a file'}
        </button>
        <span className="muted">or drop it here</span>
      </div>
      {upload.isPending && (
        <p className="muted small" role="status">
          Reading your CV. With a local model this can take a minute.
        </p>
      )}
      {upload.isError && (
        <p className="refresh-error" role="alert">
          {upload.error.message}
        </p>
      )}
    </section>
  )
}

function ProfileEditor({
  profile,
  onSaved,
  onRead,
  onDeleted,
}: {
  profile: ProfileView
  onSaved: (p: ProfileView) => void
  onRead: (r: ProfileResponse) => void
  onDeleted: () => void
}) {
  const saved = toDraft(profile)
  const [draft, setDraft] = useState<ProfileData>(saved)
  const [languages, setLanguages] = useState(profile.languages.join(', '))
  const current: ProfileData = { ...draft, languages: parseList(languages) }
  const dirty = isDirty(current, saved)

  const save = useMutation({ mutationFn: () => api.saveProfile(current), onSuccess: onSaved })
  const reread = useMutation({ mutationFn: api.reextract, onSuccess: onRead })
  const remove = useMutation({ mutationFn: api.deleteProfile, onSuccess: onDeleted })

  const set = <K extends keyof ProfileData>(key: K, value: ProfileData[K]) => setDraft((d) => ({ ...d, [key]: value }))
  const seniorities: readonly string[] = SENIORITIES.includes(draft.seniority as never) ? SENIORITIES : [draft.seniority, ...SENIORITIES]

  return (
    <section className="card" aria-labelledby="profile-h">
      <div className="card-head">
        <h2 id="profile-h">Your profile</h2>
        <p>
          {profile.cv_filename ? `${profile.cv_filename} · ` : ''}
          {profile.updated_at ? `updated ${relativeTime(profile.updated_at)} · ` : ''}
          {extractedByLabel(profile.extracted_by)}
        </p>
      </div>

      <form
        className="profile-form"
        onSubmit={(e) => {
          e.preventDefault()
          if (dirty) save.mutate()
        }}
      >
        <label>
          <span>Headline</span>
          <input type="text" value={draft.headline} onChange={(e) => set('headline', e.target.value)} placeholder="e.g. Senior backend engineer" />
        </label>
        <div className="row">
          <label>
            <span>Seniority</span>
            <select value={draft.seniority} onChange={(e) => set('seniority', e.target.value)}>
              {seniorities.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>Years of experience</span>
            <input
              type="number"
              min={0}
              max={60}
              value={draft.years_experience}
              onChange={(e) => set('years_experience', Number.parseInt(e.target.value, 10) || 0)}
            />
          </label>
        </div>

        <div className="field">
          <span className="label">Skills</span>
          <SkillsEditor skills={draft.skills} onChange={(s) => set('skills', s)} />
        </div>

        <label>
          <span>Spoken languages</span>
          <input type="text" value={languages} onChange={(e) => setLanguages(e.target.value)} placeholder="English, Romanian" />
        </label>
        <label>
          <span>Summary</span>
          <textarea rows={3} value={draft.summary} onChange={(e) => set('summary', e.target.value)} />
        </label>

        {save.isError && (
          <p className="refresh-error" role="alert">
            {save.error.message}
          </p>
        )}
        <div className="form-actions">
          <button type="submit" className="btn primary" disabled={!dirty || save.isPending}>
            {save.isPending ? 'Saving…' : 'Save changes'}
          </button>
          {dirty && (
            <button
              type="button"
              className="btn"
              onClick={() => {
                setDraft(saved)
                setLanguages(profile.languages.join(', '))
              }}
            >
              Discard changes
            </button>
          )}
          <span className="spacer" />
          <button
            type="button"
            className="btn"
            disabled={reread.isPending}
            onClick={() => {
              if (!dirty || window.confirm('Re-reading your CV replaces your edits. Continue?')) reread.mutate()
            }}
          >
            {reread.isPending ? 'Reading…' : 'Re-read CV'}
          </button>
          <button
            type="button"
            className="btn danger"
            disabled={remove.isPending}
            onClick={() => {
              if (window.confirm('Delete your CV and profile from this computer? Job scores will go away.')) remove.mutate()
            }}
          >
            Delete CV
          </button>
        </div>
        {(reread.isError || remove.isError) && (
          <p className="refresh-error" role="alert">
            {(reread.error ?? remove.error)?.message}
          </p>
        )}
      </form>

      {profile.cv_preview && (
        <details className="cv-preview">
          <summary>
            Check what was read from your CV ({(profile.cv_chars ?? 0).toLocaleString()} characters)
          </summary>
          <p className="muted small">If this looks garbled or empty, the file may be image-based. Export a text PDF or DOCX instead.</p>
          <pre>{profile.cv_preview}</pre>
        </details>
      )}
    </section>
  )
}
