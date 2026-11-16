import { fireEvent, render, screen } from '@testing-library/react'
import type { FitStatus } from '../api'
import { MatchStatus } from './MatchStatus'

const status = (over: { ollama?: Partial<FitStatus['ollama']>; index?: Partial<FitStatus['index']> } = {}): FitStatus => ({
  ollama: {
    enabled: true, reachable: true, version: '0.9.0', embed_model: 'nomic-embed-text', embed_installed: true,
    chat_model: 'qwen2.5:7b', chat_installed: true, installed: [], ...over.ollama,
  },
  has_profile: true,
  index: { eligible: 100, with_skills: 100, with_embeddings: 100, syncing: false, last_sync: null, ...over.index },
})

const show = (s: FitStatus, onSync = vi.fn(), syncing = false) => render(<MatchStatus status={s} onSync={onSync} syncing={syncing} />)

describe('MatchStatus', () => {
  it('reports a healthy setup without any setup help', () => {
    show(status())
    expect(screen.getByText('Running (Ollama 0.9.0)')).toBeInTheDocument()
    expect(screen.getByText('nomic-embed-text installed')).toBeInTheDocument()
    expect(screen.getByText('qwen2.5:7b installed')).toBeInTheDocument()
    expect(screen.getByText(/100 of 100 have skills, 100 have wording scores/)).toBeInTheDocument()
    expect(screen.queryByText(/how to set up/i)).not.toBeInTheDocument()
  })

  it('says the CV stays on this computer', () => {
    show(status())
    expect(screen.getByText(/stored on this computer only/i)).toBeInTheDocument()
  })

  it('tells you how to install Ollama when it is not running', () => {
    show(status({ ollama: { reachable: false, embed_installed: false, chat_installed: false } }))
    expect(screen.getByText(/not running\. jobs are matched on skills only/i)).toBeInTheDocument()
    const help = screen.getByText(/how to set up the local model/i).closest('details')!
    expect(help).toHaveAttribute('open')
    expect(help).toHaveTextContent('brew install ollama')
    expect(help).toHaveTextContent('make ollama')
    expect(help).toHaveTextContent('ollama pull nomic-embed-text')
    expect(help).toHaveTextContent('ollama pull qwen2.5:7b')
    expect(help).toHaveTextContent('OLLAMA_NO_CLOUD=1')
  })

  it('only asks for the models that are missing', () => {
    show(status({ ollama: { embed_installed: false } }))
    const help = screen.getByText(/how to set up the local model/i).closest('details')!
    expect(help).toHaveTextContent('ollama pull nomic-embed-text')
    expect(help).not.toHaveTextContent('brew install')
    expect(help).not.toHaveTextContent('ollama pull qwen2.5:7b')
  })

  it('names a configured chat model that is missing', () => {
    show(status({ ollama: { chat_installed: false, chat_model: 'gemma3:12b' } }))
    expect(screen.getByText(/ollama pull gemma3:12b/)).toBeInTheDocument()
  })

  it('explains when Ollama is turned off in the config', () => {
    show(status({ ollama: { enabled: false, reachable: false } }))
    expect(screen.getByText(/turned off in the config/i)).toBeInTheDocument()
    expect(screen.queryByText(/how to set up/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/have wording scores/)).not.toBeInTheDocument()
  })

  it('shows indexing progress and disables re-indexing while it runs', () => {
    show(status({ index: { with_skills: 40, with_embeddings: 10, syncing: true } }))
    expect(screen.getByText(/40 of 100 have skills, 10 have wording scores · indexing…/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Indexing…' })).toBeDisabled()
  })

  it('starts a re-index', () => {
    const onSync = vi.fn()
    show(status(), onSync)
    fireEvent.click(screen.getByRole('button', { name: 'Re-index jobs' }))
    expect(onSync).toHaveBeenCalledTimes(1)
  })

  it('shows the last indexing problem', () => {
    show(status({ index: { last_error: 'Ollama is not running, so jobs are matched on skills only.' } }))
    expect(screen.getByRole('status')).toHaveTextContent('matched on skills only')
  })
})
