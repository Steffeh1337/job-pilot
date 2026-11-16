import type { Profile, ProfileSkill, ProfileView } from '../api'

export const SENIORITIES = ['junior', 'mid', 'senior', 'staff', 'principal'] as const

/** Adds a skill unless it is blank or already there, ignoring case. */
export function addSkill(skills: ProfileSkill[], name: string): ProfileSkill[] {
  const trimmed = name.trim()
  if (!trimmed || skills.some((s) => s.name.toLowerCase() === trimmed.toLowerCase())) return skills
  return [...skills, { name: trimmed, core: false }]
}

export function removeSkill(skills: ProfileSkill[], name: string): ProfileSkill[] {
  return skills.filter((s) => s.name !== name)
}

export function toggleCore(skills: ProfileSkill[], name: string): ProfileSkill[] {
  return skills.map((s) => (s.name === name ? { ...s, core: !s.core } : s))
}

/** Splits "English, Romanian" (or lines) into a list without blanks or repeats. */
export function parseList(text: string): string[] {
  const out: string[] = []
  for (const part of text.split(/[,\n;]/)) {
    const item = part.trim()
    if (item && !out.some((x) => x.toLowerCase() === item.toLowerCase())) out.push(item)
  }
  return out
}

/** The editable part of a stored profile. */
export function toDraft(p: ProfileView): Profile {
  return {
    headline: p.headline,
    seniority: p.seniority,
    years_experience: p.years_experience,
    skills: p.skills.map((s) => ({ name: s.name, core: s.core })),
    languages: [...p.languages],
    summary: p.summary,
    highlights: [...(p.highlights ?? [])],
  }
}

/** Whether the draft differs from what is stored. Skill order doesn't matter. */
export function isDirty(draft: Profile, saved: Profile): boolean {
  const key = (p: Profile) =>
    JSON.stringify({
      h: p.headline.trim(),
      s: p.seniority,
      y: Number(p.years_experience) || 0,
      k: [...p.skills].map((x) => `${x.name.toLowerCase()}:${x.core ? 1 : 0}`).sort(),
      l: p.languages.map((x) => x.toLowerCase()).sort(),
      m: p.summary.trim(),
      b: p.highlights.map((x) => x.trim()).filter(Boolean),
    })
  return key(draft) !== key(saved)
}

/** How the profile was read, in words. */
export function extractedByLabel(by?: string): string {
  if (!by) return ''
  if (by.startsWith('llm:')) return `Read by ${by.slice(4)} on this computer`
  return 'Read from keywords (no local model was used)'
}
