import { useState } from 'react'
import type { ProfileSkill } from '../api'
import { addSkill, removeSkill, toggleCore } from '../lib/profile'

/**
 * Edits the skill list. The star marks a core skill: one used day to day,
 * which counts more in the match than one you merely know.
 */
export function SkillsEditor({ skills, onChange }: { skills: ProfileSkill[]; onChange: (s: ProfileSkill[]) => void }) {
  const [text, setText] = useState('')

  function add() {
    const next = addSkill(skills, text)
    if (next !== skills) onChange(next)
    setText('')
  }

  return (
    <div>
      {skills.length === 0 ? (
        <p className="muted small">No skills yet. Add the ones you want jobs matched on.</p>
      ) : (
        <ul className="chips" aria-label="Skills">
          {skills.map((s) => (
            <li key={s.name} className={`chip${s.core ? ' core' : ''}`}>
              <button
                type="button"
                className="chip-star"
                aria-pressed={s.core}
                aria-label={`${s.name}: ${s.core ? 'core skill' : 'known skill'}. Toggle.`}
                title={s.core ? 'Core skill: counts more' : 'Mark as a core skill (used day to day)'}
                onClick={() => onChange(toggleCore(skills, s.name))}
              >
                {s.core ? '★' : '☆'}
              </button>
              <span>{s.name}</span>
              <button type="button" className="chip-x" aria-label={`Remove ${s.name}`} onClick={() => onChange(removeSkill(skills, s.name))}>
                ×
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="add-skill">
        <input
          type="text"
          aria-label="Add a skill"
          placeholder="Add a skill and press Enter"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              add()
            }
          }}
        />
        <button type="button" className="btn" onClick={add} disabled={!text.trim()}>
          Add
        </button>
      </div>
      <p className="muted small">★ marks skills you use day to day. They count more than ones you merely know.</p>
    </div>
  )
}
