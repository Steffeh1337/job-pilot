import { fireEvent, screen, waitFor } from '@testing-library/react'
import type { ProfileView } from '../api'
import { fakeApi, json, renderApp } from '../testing'
import { Ask } from './Ask'

const none: ProfileView = { exists: false, headline: '', seniority: '', years_experience: 0, skills: [], languages: [], summary: '', highlights: [], has_embedding: false }
const saved: ProfileView = { ...none, exists: true, headline: 'Senior backend engineer' }

const sent = (api: ReturnType<typeof fakeApi>) => JSON.parse(String(api.to('POST', '/api/ask')[0]?.body)) as { question: string; use_cv: boolean }

const type = (text: string) => fireEvent.change(screen.getByLabelText('Your question'), { target: { value: text } })

describe('Ask page', () => {
  it('sends the question with the CV, shows the answer and clears the box', async () => {
    const api = fakeApi({ 'GET /api/profile': saved, 'POST /api/ask': { answer: 'I led the Go migration.', used_cv: true } })
    renderApp(<Ask />)
    const cv = await screen.findByLabelText('Use my CV as context')
    await waitFor(() => expect(cv).toBeEnabled())
    expect(cv).toBeChecked()

    type('  Tell me about high load  ')
    fireEvent.click(screen.getByRole('button', { name: 'Ask' }))

    expect(await screen.findByText('I led the Go migration.')).toBeInTheDocument()
    expect(screen.getByText('Tell me about high load')).toBeInTheDocument()
    expect(screen.getByText('Answered from your CV')).toBeInTheDocument()
    expect(sent(api)).toEqual({ question: 'Tell me about high load', use_cv: true })
    expect(screen.getByLabelText('Your question')).toHaveValue('')
  })

  it('keeps the CV out when the box is unticked', async () => {
    const api = fakeApi({ 'GET /api/profile': saved, 'POST /api/ask': { answer: 'A goroutine is a light thread.', used_cv: false } })
    renderApp(<Ask />)
    const cv = await screen.findByLabelText('Use my CV as context')
    await waitFor(() => expect(cv).toBeEnabled())
    fireEvent.click(cv)
    type('What is a goroutine?')
    fireEvent.click(screen.getByRole('button', { name: 'Ask' }))

    expect(await screen.findByText('Answered without your CV')).toBeInTheDocument()
    expect(sent(api).use_cv).toBe(false)
  })

  it('cannot use a CV that has not been uploaded', async () => {
    const api = fakeApi({ 'GET /api/profile': none, 'POST /api/ask': { answer: 'ok', used_cv: false } })
    renderApp(<Ask />)
    expect(await screen.findByText(/Upload your CV on the Profile page/)).toBeInTheDocument()
    const cv = screen.getByLabelText('Use my CV as context')
    expect(cv).toBeDisabled()
    expect(cv).not.toBeChecked()
    type('Hello?')
    fireEvent.click(screen.getByRole('button', { name: 'Ask' }))
    await screen.findByText('ok')
    expect(sent(api).use_cv).toBe(false)
  })

  it('will not send a blank question, and says when the model is unavailable', async () => {
    fakeApi({ 'GET /api/profile': saved, 'POST /api/ask': () => json({ error: 'Ollama is not running. Start it (make ollama) and try again.' }, 503) })
    renderApp(<Ask />)
    const send = await screen.findByRole('button', { name: 'Ask' })
    expect(send).toBeDisabled()
    type('Hello?')
    fireEvent.click(send)
    expect(await screen.findByRole('alert')).toHaveTextContent('Ollama is not running')
    expect(screen.getByLabelText('Your question')).toHaveValue('Hello?')
  })
})
