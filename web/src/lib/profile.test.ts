import type { Profile, ProfileView } from '../api'
import { addSkill, extractedByLabel, isDirty, parseList, removeSkill, toDraft, toggleCore } from './profile'

const skills = [{ name: 'Go', core: true }, { name: 'Docker', core: false }]

describe('skill editing', () => {
  it('adds a trimmed skill as not core', () =>
    expect(addSkill(skills, '  Temporal ')).toEqual([...skills, { name: 'Temporal', core: false }]))
  it('ignores blanks', () => expect(addSkill(skills, '   ')).toBe(skills))
  it('ignores duplicates in any case', () => expect(addSkill(skills, 'go')).toBe(skills))
  it('removes by name', () => expect(removeSkill(skills, 'Go')).toEqual([{ name: 'Docker', core: false }]))
  it('toggles core without touching the others', () =>
    expect(toggleCore(skills, 'Docker')).toEqual([{ name: 'Go', core: true }, { name: 'Docker', core: true }]))
  it('does not mutate its input', () => {
    toggleCore(skills, 'Go')
    expect(skills[0]!.core).toBe(true)
  })
})

describe('parseList', () => {
  it.each([
    ['English, Romanian', ['English', 'Romanian']],
    ['English;french\nGerman', ['English', 'french', 'German']],
    [' english , ENGLISH ,, ', ['english']],
    ['', []],
  ])('%j -> %j', (text, want) => expect(parseList(text)).toEqual(want))
})

const view = (over: Partial<ProfileView> = {}): ProfileView => ({
  exists: true, headline: 'Engineer', seniority: 'senior', years_experience: 8,
  skills: [{ name: 'Go', core: true, category: 'Languages' }], languages: ['English'], summary: 'Hello', highlights: ['Led the Go migration'],
  has_embedding: false, ...over,
})

describe('toDraft and isDirty', () => {
  it('drops server-only fields from the draft', () => {
    expect(toDraft(view())).toEqual({
      headline: 'Engineer', seniority: 'senior', years_experience: 8,
      skills: [{ name: 'Go', core: true }], languages: ['English'], summary: 'Hello',
      highlights: ['Led the Go migration'],
    })
  })

  it('is clean when nothing changed', () => expect(isDirty(toDraft(view()), toDraft(view()))).toBe(false))

  const base = toDraft(view())
  it.each<[string, Partial<Profile>]>([
    ['headline', { headline: 'Lead' }],
    ['seniority', { seniority: 'staff' }],
    ['years', { years_experience: 9 }],
    ['a new skill', { skills: [...base.skills, { name: 'Rust', core: false }] }],
    ['a core flag', { skills: [{ name: 'Go', core: false }] }],
    ['languages', { languages: ['English', 'Romanian'] }],
    ['summary', { summary: 'Changed' }],
    ['a highlight', { highlights: ['Led the Go migration', 'Ran the platform team'] }],
    ['an edited highlight', { highlights: ['Led the Rust migration'] }],
  ])('notices a change to %s', (_, change) => expect(isDirty({ ...base, ...change }, base)).toBe(true))

  it('ignores skill order, whitespace and case in names', () => {
    const a = { ...base, skills: [{ name: 'Go', core: true }, { name: 'Docker', core: false }], headline: ' Engineer ' }
    const b = { ...base, skills: [{ name: 'docker', core: false }, { name: 'GO', core: true }] }
    expect(isDirty(a, b)).toBe(false)
  })

  it('ignores blank highlight rows and surrounding whitespace', () =>
    expect(isDirty({ ...base, highlights: ['  Led the Go migration ', '', '   '] }, base)).toBe(false))

  it('treats a profile saved before highlights existed as having none', () => {
    const old = { ...view(), highlights: undefined } as unknown as ProfileView
    expect(toDraft(old).highlights).toEqual([])
  })
})

describe('extractedByLabel', () => {
  it('names the model', () => expect(extractedByLabel('llm:qwen2.5:7b')).toBe('Read by qwen2.5:7b on this computer'))
  it('says keywords plainly', () => expect(extractedByLabel('keywords')).toMatch(/keywords/))
  it('is empty when unknown', () => expect(extractedByLabel(undefined)).toBe(''))
})
