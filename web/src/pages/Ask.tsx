import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api'

interface Exchange {
  question: string
  answer: string
  usedCv: boolean
}

/**
 * A prompt box for the local model. With "Use my CV" on, the stored CV is
 * given to it as context, so it can answer questions about your own
 * experience. Everything runs on this computer, and the conversation is only
 * kept on this page: nothing is stored, and reloading clears it.
 */
export function Ask() {
  const profile = useQuery({ queryKey: ['profile'], queryFn: api.profile })
  const hasCv = profile.data?.exists === true
  const [question, setQuestion] = useState('')
  const [useCv, setUseCv] = useState(true)
  const [history, setHistory] = useState<Exchange[]>([])
  const [copied, setCopied] = useState<number | null>(null)

  const withCv = hasCv && useCv
  const ask = useMutation({
    mutationFn: ({ q, cv }: { q: string; cv: boolean }) => api.ask(q, cv),
    onSuccess: (res, { q }) => {
      setHistory((h) => [{ question: q, answer: res.answer, usedCv: res.used_cv }, ...h])
      setQuestion('')
    },
  })

  const canSend = question.trim() !== '' && !ask.isPending

  function send() {
    if (canSend) ask.mutate({ q: question.trim(), cv: withCv })
  }

  async function copy(i: number) {
    const text = history[i]?.answer
    if (text === undefined) return
    try {
      await navigator.clipboard.writeText(text)
      setCopied(i)
    } catch {
      // clipboard blocked; the text is still selectable
    }
  }

  return (
    <>
      <div className="page-head">
        <div>
          <h1>Ask</h1>
          <p>
            Ask the local model directly, for instance an application question. It runs on this computer; nothing you type
            or read here leaves it, and nothing is saved.
          </p>
        </div>
      </div>

      <section className="card">
        <form
          className="ask-form"
          onSubmit={(e) => {
            e.preventDefault()
            send()
          }}
        >
          <textarea
            rows={4}
            maxLength={4000}
            value={question}
            aria-label="Your question"
            placeholder="e.g. What is your experience with high-load systems?"
            onChange={(e) => setQuestion(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                send()
              }
            }}
          />
          <div className="ask-options">
            <label>
              <input type="checkbox" checked={withCv} disabled={!hasCv} onChange={(e) => setUseCv(e.target.checked)} /> Use my CV as
              context
            </label>
            <span className="spacer" />
            <span className="muted small">⌘/Ctrl + Enter to send</span>
            <button type="submit" className="btn primary" disabled={!canSend}>
              {ask.isPending ? 'Thinking…' : 'Ask'}
            </button>
          </div>
          {profile.isSuccess && !hasCv && (
            <p className="muted small">Upload your CV on the Profile page to let the model answer from it.</p>
          )}
          {withCv && (
            <p className="muted small">
              The model only knows what your CV says. If it doesn’t state a figure, such as requests per second, the answer
              shows a [placeholder] for you to fill in rather than a made-up number.
            </p>
          )}
          {ask.isPending && (
            <p className="muted small" role="status">
              The local model is writing. The first answer can take a minute while it loads.
            </p>
          )}
          {ask.isError && (
            <p className="refresh-error" role="alert">
              {ask.error.message}
            </p>
          )}
        </form>
      </section>

      {history.length > 0 && (
        <section className="card" aria-label="Answers">
          {history.map((x, i) => (
            <div className="exchange" key={history.length - i}>
              <p className="question">{x.question}</p>
              <p className="answer">{x.answer}</p>
              <div className="meta">
                <button type="button" className="btn" onClick={() => void copy(i)}>
                  {copied === i ? 'Copied' : 'Copy'}
                </button>
                <span className="muted small">{x.usedCv ? 'Answered from your CV' : 'Answered without your CV'}</span>
              </div>
            </div>
          ))}
        </section>
      )}
    </>
  )
}
