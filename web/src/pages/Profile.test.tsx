import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import type { FitStatus, ProfileView } from '../api'
import { fakeApi, json, renderApp } from '../testing'
import { Profile } from './Profile'

const ready: FitStatus = {
  ollama: { enabled: true, reachable: true, version: '0.9.0', embed_model: 'nomic-embed-text', embed_installed: true, chat_model: 'qwen2.5:7b', chat_installed: true, installed: [] },
  has_profile: false,
  index: { eligible: 10, with_skills: 10, with_embeddings: 10, syncing: false, last_sync: null },
}

const none: ProfileView = { exists: false, headline: '', seniority: '', years_experience: 0, skills: [], languages: [], summary: '', highlights: [], has_embedding: false }

const saved: ProfileView = {
  exists: true, headline: 'Senior backend engineer', seniority: 'senior', years_experience: 8,
  skills: [{ name: 'Go', core: true, category: 'Languages' }, { name: 'Docker', core: false }],
  languages: ['English'], summary: 'Builds backend systems.', highlights: [], has_embedding: true,
  cv_filename: 'jane.pdf', cv_chars: 1234, cv_preview: 'Jane Developer\nSenior Backend Engineer',
  extracted_by: 'llm:qwen2.5:7b', updated_at: '2026-11-20T10:00:00Z', uploaded_at: '2026-11-20T10:00:00Z',
}

function cvFile(name = 'jane.pdf') {
  return new File(['%PDF-1.4 fake'], name, { type: 'application/pdf' })
}

const chooseFile = (file: File) => fireEvent.change(screen.getByLabelText('Choose your CV'), { target: { files: [file] } })

describe('Profile page: before a CV', () => {
  it('offers the upload and no editor', async () => {
    fakeApi({ 'GET /api/profile': none, 'GET /api/fit/status': ready })
    renderApp(<Profile />)
    expect(await screen.findByRole('heading', { name: 'Upload your CV' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Your profile' })).not.toBeInTheDocument()
    expect(screen.getByText(/PDF, DOCX or plain text, up to 8 MB/)).toBeInTheDocument()
  })

  it('uploads the file as a "cv" form field, then shows the editor and any warnings', async () => {
    const api = fakeApi({
      'GET /api/profile': none,
      'GET /api/fit/status': ready,
      'POST /api/profile/cv': { profile: saved, warnings: ['Ollama is not running, so the profile was built from keywords only.'] },
    })
    renderApp(<Profile />)
    await screen.findByRole('heading', { name: 'Upload your CV' })
    chooseFile(cvFile())

    expect(await screen.findByRole('heading', { name: 'Your profile' })).toBeInTheDocument()
    expect(screen.getByText(/Ollama is not running, so the profile was built from keywords only/)).toBeInTheDocument()
    const post = api.to('POST', '/api/profile/cv')
    expect(post).toHaveLength(1)
    const sent = post[0]!.body as FormData
    expect(sent.get('cv')).toBeInstanceOf(File)
    expect((sent.get('cv') as File).name).toBe('jane.pdf')
    expect(screen.getByDisplayValue('Senior backend engineer')).toBeInTheDocument()
    expect(screen.getByText(/jane\.pdf/)).toBeInTheDocument()
    expect(screen.getByText('Read by qwen2.5:7b on this computer', { exact: false })).toBeInTheDocument()
  })

  it('shows the server’s reason when a file is rejected, and keeps the upload available', async () => {
    fakeApi({
      'GET /api/profile': none,
      'GET /api/fit/status': ready,
      'POST /api/profile/cv': () => json({ error: 'no readable text found: the file may be a scan or an image' }, 422),
    })
    renderApp(<Profile />)
    await screen.findByRole('heading', { name: 'Upload your CV' })
    chooseFile(cvFile('scan.pdf'))
    expect(await screen.findByRole('alert')).toHaveTextContent('no readable text found')
    expect(screen.getByRole('button', { name: 'Choose a file' })).toBeEnabled()
    expect(screen.queryByRole('heading', { name: 'Your profile' })).not.toBeInTheDocument()
  })

  it('shows progress while a local model reads the CV', async () => {
    let finish: (r: Response) => void = () => {}
    fakeApi({
      'GET /api/profile': none,
      'GET /api/fit/status': ready,
      'POST /api/profile/cv': () => new Promise<Response>((r) => (finish = r)),
    })
    renderApp(<Profile />)
    await screen.findByRole('heading', { name: 'Upload your CV' })
    chooseFile(cvFile())
    expect(await screen.findByRole('button', { name: 'Reading your CV…' })).toBeDisabled()
    finish(json({ profile: saved, warnings: [] }))
    expect(await screen.findByRole('heading', { name: 'Your profile' })).toBeInTheDocument()
  })
})

describe('Profile page: editing', () => {
  function setup(extra: Record<string, unknown> = {}) {
    const api = fakeApi({ 'GET /api/profile': saved, 'GET /api/fit/status': ready, ...extra })
    renderApp(<Profile />)
    return api
  }

  it('keeps Save disabled until something changes', async () => {
    setup()
    const save = await screen.findByRole('button', { name: 'Save changes' })
    expect(save).toBeDisabled()
    fireEvent.change(screen.getByDisplayValue('Senior backend engineer'), { target: { value: 'Staff engineer' } })
    expect(save).toBeEnabled()
    fireEvent.change(screen.getByDisplayValue('Staff engineer'), { target: { value: 'Senior backend engineer' } })
    expect(save).toBeDisabled()
  })

  it('saves the edited profile as JSON, then goes back to clean', async () => {
    const updated = { ...saved, headline: 'Staff engineer', updated_at: '2026-11-20T11:00:00Z', skills: [{ name: 'Go', core: true }, { name: 'Docker', core: true }, { name: 'Temporal', core: false }] }
    const api = setup({ 'PUT /api/profile': () => json(updated) })
    await screen.findByRole('button', { name: 'Save changes' })

    fireEvent.change(screen.getByDisplayValue('Senior backend engineer'), { target: { value: 'Staff engineer' } })
    fireEvent.change(screen.getByLabelText('Add a skill'), { target: { value: 'Temporal' } })
    fireEvent.keyDown(screen.getByLabelText('Add a skill'), { key: 'Enter' })
    fireEvent.click(screen.getByRole('button', { name: /Docker: known skill/ }))
    fireEvent.change(screen.getByPlaceholderText('English, Romanian'), { target: { value: 'English, Romanian' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(api.to('PUT', '/api/profile')).toHaveLength(1))
    const call = api.to('PUT', '/api/profile')[0]!
    expect(JSON.parse(String(call.body))).toEqual({
      headline: 'Staff engineer', seniority: 'senior', years_experience: 8,
      skills: [{ name: 'Go', core: true }, { name: 'Docker', core: true }, { name: 'Temporal', core: false }],
      languages: ['English', 'Romanian'], summary: 'Builds backend systems.', highlights: [],
    })
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled())
    expect(screen.getByDisplayValue('Staff engineer')).toBeInTheDocument()
  })

  it('shows a save error and keeps the edits', async () => {
    setup({ 'PUT /api/profile': () => json({ error: 'seniority must be junior, mid, senior, staff or principal' }, 400) })
    await screen.findByRole('button', { name: 'Save changes' })
    fireEvent.change(screen.getByDisplayValue('Senior backend engineer'), { target: { value: 'Changed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('seniority must be')
    expect(screen.getByDisplayValue('Changed')).toBeInTheDocument()
  })

  it('discards edits', async () => {
    setup()
    await screen.findByRole('button', { name: 'Save changes' })
    fireEvent.change(screen.getByDisplayValue('Senior backend engineer'), { target: { value: 'Changed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(screen.getByDisplayValue('Senior backend engineer')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Discard changes' })).not.toBeInTheDocument()
  })

  it('shows what was read from the CV, with a hint for image-based files', async () => {
    setup()
    const details = (await screen.findByText(/check what was read from your cv/i)).closest('details')!
    expect(within(details).getByText(/1,234 characters/)).toBeInTheDocument()
    expect(details).toHaveTextContent('Senior Backend Engineer')
    expect(details).toHaveTextContent(/image-based/)
  })
})

describe('Profile page: highlights', () => {
  const drafted = { highlights: ['Led the migration to Go microservices on Kubernetes.', 'Ran Docker in production for 8 years.'], warnings: [] }

  function setup(profile: ProfileView, extra: Record<string, unknown> = {}) {
    const api = fakeApi({ 'GET /api/profile': profile, 'GET /api/fit/status': ready, ...extra })
    renderApp(<Profile />)
    return api
  }

  it('drafts bullets into the form without saving them', async () => {
    const api = setup(saved, { 'POST /api/profile/highlights': drafted })
    fireEvent.click(await screen.findByRole('button', { name: 'Draft from my CV' }))

    expect(await screen.findByLabelText('Highlight 1')).toHaveValue(drafted.highlights[0])
    expect(screen.getByLabelText('Highlight 2')).toHaveValue(drafted.highlights[1])
    expect(api.to('PUT', '/api/profile')).toHaveLength(0)
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled()
  })

  it('saves the highlights with the profile', async () => {
    const api = setup(saved, { 'POST /api/profile/highlights': drafted, 'PUT /api/profile': () => json({ ...saved, highlights: drafted.highlights }) })
    fireEvent.click(await screen.findByRole('button', { name: 'Draft from my CV' }))
    await screen.findByLabelText('Highlight 1')
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(api.to('PUT', '/api/profile')).toHaveLength(1))
    expect(JSON.parse(String(api.to('PUT', '/api/profile')[0]!.body)).highlights).toEqual(drafted.highlights)
  })

  it('passes on what the model could not back up', async () => {
    setup(saved, { 'POST /api/profile/highlights': { highlights: [], warnings: ['The model found no achievement it could back up with your CV.'] } })
    fireEvent.click(await screen.findByRole('button', { name: 'Draft from my CV' }))
    expect(await screen.findByText(/could back up with your CV/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Highlight 1')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Save changes' })).toBeDisabled()
  })

  it('explains when the local model is not available', async () => {
    setup(saved, { 'POST /api/profile/highlights': () => json({ error: 'Ollama is not running. Start it (make ollama) and try again.' }, 503) })
    fireEvent.click(await screen.findByRole('button', { name: 'Draft from my CV' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Ollama is not running')
  })

  it('asks before replacing bullets you wrote, and respects a no', async () => {
    const api = setup({ ...saved, highlights: ['My own bullet'] }, { 'POST /api/profile/highlights': drafted })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    fireEvent.click(await screen.findByRole('button', { name: 'Draft from my CV' }))
    expect(confirm).toHaveBeenCalled()
    expect(api.to('POST', '/api/profile/highlights')).toHaveLength(0)
    expect(screen.getByLabelText('Highlight 1')).toHaveValue('My own bullet')
  })

  it('lets you write, add up to three, and remove bullets', async () => {
    setup(saved)
    fireEvent.click(await screen.findByRole('button', { name: 'Add a highlight' }))
    fireEvent.change(screen.getByLabelText('Highlight 1'), { target: { value: 'Cut deploy time by 70%' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add a highlight' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add a highlight' }))
    expect(screen.queryByRole('button', { name: 'Add a highlight' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Remove highlight 3' }))
    expect(screen.getByRole('button', { name: 'Add a highlight' })).toBeInTheDocument()
    expect(screen.getByLabelText('Highlight 1')).toHaveValue('Cut deploy time by 70%')
  })
})

describe('Profile page: re-reading and deleting', () => {
  afterEach(() => vi.restoreAllMocks())

  it('re-reads the CV without asking when there are no edits', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    const api = fakeApi({
      'GET /api/profile': saved, 'GET /api/fit/status': ready,
      'POST /api/profile/reextract': { profile: { ...saved, updated_at: '2026-11-20T12:00:00Z' }, warnings: [] },
    })
    renderApp(<Profile />)
    fireEvent.click(await screen.findByRole('button', { name: 'Re-read CV' }))
    await waitFor(() => expect(api.to('POST', '/api/profile/reextract')).toHaveLength(1))
    expect(confirm).not.toHaveBeenCalled()
  })

  it('asks before re-reading over unsaved edits, and respects a no', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const api = fakeApi({ 'GET /api/profile': saved, 'GET /api/fit/status': ready, 'POST /api/profile/reextract': { profile: saved, warnings: [] } })
    renderApp(<Profile />)
    await screen.findByRole('button', { name: 'Re-read CV' })
    fireEvent.change(screen.getByDisplayValue('Senior backend engineer'), { target: { value: 'Mine' } })
    fireEvent.click(screen.getByRole('button', { name: 'Re-read CV' }))
    expect(confirm).toHaveBeenCalledWith(expect.stringMatching(/replaces your edits/))
    expect(api.to('POST', '/api/profile/reextract')).toHaveLength(0)
  })

  it('deletes the CV after confirmation and goes back to the upload', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const api = fakeApi({
      'GET /api/profile': saved, 'GET /api/fit/status': ready,
      'DELETE /api/profile': () => new Response(null, { status: 204 }),
    })
    renderApp(<Profile />)
    fireEvent.click(await screen.findByRole('button', { name: 'Delete CV' }))
    await waitFor(() => expect(api.to('DELETE', '/api/profile')).toHaveLength(1))
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Your profile' })).not.toBeInTheDocument())
    expect(screen.getByRole('heading', { name: 'Upload your CV' })).toBeInTheDocument()
  })

  it('does not delete when you cancel', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const api = fakeApi({ 'GET /api/profile': saved, 'GET /api/fit/status': ready })
    renderApp(<Profile />)
    fireEvent.click(await screen.findByRole('button', { name: 'Delete CV' }))
    expect(api.to('DELETE', '/api/profile')).toHaveLength(0)
    expect(screen.getByRole('heading', { name: 'Your profile' })).toBeInTheDocument()
  })
})

describe('Profile page: status', () => {
  it('shows setup help when Ollama is not running', async () => {
    fakeApi({
      'GET /api/profile': none,
      'GET /api/fit/status': { ...ready, ollama: { ...ready.ollama, reachable: false, embed_installed: false, chat_installed: false } },
    })
    renderApp(<Profile />)
    expect(await screen.findByText(/how to set up the local model/i)).toBeInTheDocument()
    expect(screen.getByText(/brew install ollama/)).toBeInTheDocument()
  })

  it('re-indexes on request', async () => {
    const api = fakeApi({
      'GET /api/profile': none, 'GET /api/fit/status': ready,
      'POST /api/fit/sync': () => json({ ...ready, index: { ...ready.index, syncing: true } }, 202),
    })
    renderApp(<Profile />)
    fireEvent.click(await screen.findByRole('button', { name: 'Re-index jobs' }))
    await waitFor(() => expect(api.to('POST', '/api/fit/sync')).toHaveLength(1))
    expect(await screen.findByRole('button', { name: 'Indexing…' })).toBeDisabled()
  })
})
