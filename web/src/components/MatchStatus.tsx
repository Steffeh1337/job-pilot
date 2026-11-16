import type { FitStatus } from '../api'

const PULL_EMBED = (m: string) => `ollama pull ${m}`

/** Whether local matching is ready, and what to do if it isn't. */
export function MatchStatus({ status, onSync, syncing }: { status: FitStatus; onSync: () => void; syncing: boolean }) {
  const { ollama, index } = status
  const embedModel = ollama.embed_model
  const chatModel = ollama.chat_model || 'qwen2.5:7b'
  const needsSetup = ollama.enabled && (!ollama.reachable || !ollama.embed_installed || !ollama.chat_installed)

  return (
    <section className="card" aria-labelledby="status-h">
      <div className="card-head">
        <h2 id="status-h">Local matching</h2>
        <p>Your CV is read and stored on this computer only. It is only ever sent to your own local Ollama.</p>
      </div>

      <ul className="checks">
        <li className={ollama.enabled && ollama.reachable ? 'ok' : 'warn'}>
          <strong>Local model server</strong>
          <span>
            {!ollama.enabled
              ? 'Turned off in the config. Jobs are matched on skills only.'
              : ollama.reachable
                ? `Running${ollama.version ? ` (Ollama ${ollama.version})` : ''}`
                : 'Not running. Jobs are matched on skills only until you start it.'}
          </span>
        </li>
        {ollama.enabled && ollama.reachable && (
          <>
            <li className={ollama.embed_installed ? 'ok' : 'warn'}>
              <strong>Wording model</strong>
              <span>
                {ollama.embed_installed ? `${embedModel} installed` : `${embedModel} is not installed, so there is no wording score`}
              </span>
            </li>
            <li className={ollama.chat_installed ? 'ok' : 'warn'}>
              <strong>CV reader</strong>
              <span>
                {ollama.chat_installed ? `${ollama.chat_model} installed` : 'No chat model installed, so the CV is read from keywords only'}
              </span>
            </li>
          </>
        )}
        <li className={index.eligible > 0 && index.with_skills === index.eligible ? 'ok' : 'warn'}>
          <strong>Jobs indexed</strong>
          <span>
            {index.with_skills.toLocaleString()} of {index.eligible.toLocaleString()} have skills
            {ollama.enabled && ollama.reachable ? `, ${index.with_embeddings.toLocaleString()} have wording scores` : ''}
            {index.syncing && ' · indexing…'}
          </span>
        </li>
      </ul>

      {index.last_error && !index.syncing && (
        <p className="note" role="status">
          {index.last_error}
        </p>
      )}

      <div className="status-actions">
        <button className="btn" onClick={onSync} disabled={syncing || index.syncing}>
          {syncing || index.syncing ? 'Indexing…' : 'Re-index jobs'}
        </button>
      </div>

      {needsSetup && (
        <details className="setup" open={!ollama.reachable}>
          <summary>How to set up the local model</summary>
          <ol>
            {!ollama.reachable && (
              <li>
                Install Ollama if you haven’t (<code>brew install ollama</code>), then start it with <code>make ollama</code>.
                It only listens on this computer and unloads models when idle.
              </li>
            )}
            {!ollama.embed_installed && (
              <li>
                Wording model (about 274 MB): <code>{PULL_EMBED(embedModel)}</code>
              </li>
            )}
            {!ollama.chat_installed && (
              <li>
                CV reader (about 4.7 GB): <code>{PULL_EMBED(chatModel)}</code>
              </li>
            )}
            <li>
              Keep it local: <code>make ollama</code> sets <code>OLLAMA_NO_CLOUD=1</code> so cloud models can never be
              used. If you start Ollama another way, set that yourself.
            </li>
            <li>
              Then press <em>Re-index jobs</em>, and <em>Re-read CV</em> if you have already uploaded one.
            </li>
          </ol>
        </details>
      )}
    </section>
  )
}
