import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { Fit } from '../api'
import { FitBadge } from './FitBadge'
import { FitSection } from './FitSection'

const fit = (over: Partial<Fit> = {}): Fit => ({
  score: 82, band: 'strong', matched: ['Go', 'Docker'], missing: ['Kafka'], ...over,
})
const show = (f: Fit) => render(<MemoryRouter><FitSection fit={f} /></MemoryRouter>)

describe('FitBadge', () => {
  it('shows the score and the band, with the reasoning as a tooltip', () => {
    render(<FitBadge fit={fit({ semantic: 0.5 })} />)
    const badge = screen.getByText('Strong').closest('.fit')!
    expect(badge).toHaveClass('strong')
    expect(badge).toHaveTextContent('82')
    expect(badge).toHaveAttribute('title', 'Strong fit (82/100) · 2 of 3 skills match · wording 50% similar')
  })
})

describe('FitSection', () => {
  it('lists the skills you have and the ones you lack', () => {
    show(fit())
    expect(within(screen.getByRole('list', { name: 'Matched skills' })).getAllByRole('listitem').map((l) => l.textContent)).toEqual(['Go', 'Docker'])
    expect(within(screen.getByRole('list', { name: 'Missing skills' })).getAllByRole('listitem').map((l) => l.textContent)).toEqual(['Kafka'])
    expect(screen.getByText('On your CV (2)')).toBeInTheDocument()
    expect(screen.getByText('Not on your CV (1)')).toBeInTheDocument()
  })

  it('shows the wording similarity when there is one', () => {
    show(fit({ semantic: 0.634 }))
    expect(screen.getByText('63%')).toBeInTheDocument()
    expect(screen.queryByText(/skills only/i)).not.toBeInTheDocument()
  })

  it('says it is skills-only, and points to setup, when there is no wording score', () => {
    show(fit())
    expect(screen.getByText(/based on skills only/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /set it up/i })).toHaveAttribute('href', '/profile')
  })

  it('omits an empty group instead of showing an empty list', () => {
    show(fit({ missing: [] }))
    expect(screen.queryByRole('list', { name: 'Missing skills' })).not.toBeInTheDocument()
  })

  it('explains a posting with no recognisable skills', () => {
    show(fit({ matched: [], missing: [], semantic: 0.4 }))
    expect(screen.getByText(/doesn’t name any skills we recognise/)).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
  })
})
