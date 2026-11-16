import { render, screen, within } from '@testing-library/react'
import { FunnelChart } from './FunnelChart'

const row = { source: 'TOTAL', new: 1000, unique: 800, region: 200, role: 100, stack: 50, senior: 25, b2b: 5 }

describe('FunnelChart', () => {
  it('lists every step with its count and survival rate', () => {
    render(<FunnelChart row={row} />)
    const items = within(screen.getByRole('list', { name: /funnel/i })).getAllByRole('listitem')
    expect(items).toHaveLength(6)
    expect(items[0]).toHaveTextContent('Seen')
    expect(items[0]).toHaveTextContent('1,000')
    expect(items[2]).toHaveTextContent('Remote, open to Romania')
    expect(items[2]).toHaveTextContent('25%') // 200 of 800
    expect(items[5]).toHaveTextContent('Senior match')
    expect(items[5]).toHaveTextContent('50%') // 25 of 50
  })

  it('does not crash on an empty funnel', () => {
    render(<FunnelChart row={{ ...row, new: 0, unique: 0, region: 0, role: 0, stack: 0, senior: 0 }} />)
    expect(screen.getAllByRole('listitem')).toHaveLength(6)
  })
})
