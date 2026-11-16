import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import type { ProfileSkill } from '../api'
import { SkillsEditor } from './SkillsEditor'

function Harness({ initial, onChange }: { initial: ProfileSkill[]; onChange?: (s: ProfileSkill[]) => void }) {
  const [skills, setSkills] = useState(initial)
  return (
    <SkillsEditor
      skills={skills}
      onChange={(s) => {
        setSkills(s)
        onChange?.(s)
      }}
    />
  )
}

const names = () => screen.getAllByRole('listitem').map((li) => li.textContent?.replace(/[★☆×]/g, '').trim())

describe('SkillsEditor', () => {
  it('shows an empty state', () => {
    render(<Harness initial={[]} />)
    expect(screen.getByText(/no skills yet/i)).toBeInTheDocument()
  })

  it('adds a skill with Enter, trimming it, and clears the box', () => {
    render(<Harness initial={[{ name: 'Go', core: true }]} />)
    const box = screen.getByLabelText('Add a skill')
    fireEvent.change(box, { target: { value: '  Temporal ' } })
    fireEvent.keyDown(box, { key: 'Enter' })
    expect(names()).toEqual(['Go', 'Temporal'])
    expect(box).toHaveValue('')
  })

  it('adds a skill with the button, which stays disabled for blank input', () => {
    render(<Harness initial={[]} />)
    const add = screen.getByRole('button', { name: 'Add' })
    expect(add).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Add a skill'), { target: { value: 'Rust' } })
    expect(add).toBeEnabled()
    fireEvent.click(add)
    expect(names()).toEqual(['Rust'])
  })

  it('ignores a skill that is already there, in any case', () => {
    const onChange = vi.fn()
    render(<Harness initial={[{ name: 'Go', core: true }]} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('Add a skill'), { target: { value: 'go' } })
    fireEvent.keyDown(screen.getByLabelText('Add a skill'), { key: 'Enter' })
    expect(onChange).not.toHaveBeenCalled()
    expect(names()).toEqual(['Go'])
  })

  it('removes a skill', () => {
    render(<Harness initial={[{ name: 'Go', core: true }, { name: 'Docker', core: false }]} />)
    fireEvent.click(screen.getByRole('button', { name: 'Remove Go' }))
    expect(names()).toEqual(['Docker'])
  })

  it('toggles a core skill and says so for screen readers', () => {
    render(<Harness initial={[{ name: 'Docker', core: false }]} />)
    const star = screen.getByRole('button', { name: /Docker: known skill/ })
    expect(star).toHaveAttribute('aria-pressed', 'false')
    fireEvent.click(star)
    expect(screen.getByRole('button', { name: /Docker: core skill/ })).toHaveAttribute('aria-pressed', 'true')
  })
})
