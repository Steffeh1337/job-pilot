import { PAGE_SIZE, paramsFromQuery, queryFromParams } from './query'

describe('queryFromParams', () => {
  it('defaults to matches on page one', () => {
    const q = queryFromParams(new URLSearchParams())
    expect(q).toMatchObject({ status: 'match', page: 1, offset: 0, limit: PAGE_SIZE, b2b: false, unverified: false })
  })

  it('reads every filter', () => {
    const q = queryFromParams(
      new URLSearchParams('status=rejected&source=lever&reason=stack&b2b=1&unverified=1&q=go&days=30&page=3'),
    )
    expect(q).toMatchObject({
      status: 'rejected', source: 'lever', reason: 'stack', b2b: true, unverified: true, q: 'go', days: 30, page: 3,
      offset: 2 * PAGE_SIZE,
    })
  })

  it('ignores junk', () => {
    const q = queryFromParams(new URLSearchParams('status=bogus&page=-4&days=abc&sort=sideways&min_fit=42'))
    expect(q).toMatchObject({ status: 'match', page: 1, days: undefined, sort: 'new', minFit: 0 })
  })

  it('reads fit sorting and the minimum fit', () => {
    expect(queryFromParams(new URLSearchParams('sort=fit&min_fit=55'))).toMatchObject({ sort: 'fit', minFit: 55 })
  })
})

describe('paramsFromQuery', () => {
  it('drops defaults so URLs stay short', () => {
    expect(paramsFromQuery({ status: 'match', page: 1 }).toString()).toBe('')
  })

  it('round-trips fit sorting', () => {
    const p = paramsFromQuery({ status: 'match', sort: 'fit', minFit: 75 })
    expect(p.toString()).toBe('sort=fit&min_fit=75')
    expect(queryFromParams(p)).toMatchObject({ sort: 'fit', minFit: 75 })
    expect(paramsFromQuery({ status: 'match', sort: 'new', minFit: 0 }).toString()).toBe('')
  })

  it('round-trips', () => {
    const p = paramsFromQuery({ status: 'rejected', reason: 'role', source: 'ashby', days: 7, q: 'go dev', page: 2 })
    const q = queryFromParams(p)
    expect(q).toMatchObject({ status: 'rejected', reason: 'role', source: 'ashby', days: 7, q: 'go dev', page: 2 })
  })
})

describe('applied filter', () => {
  it('reads applied=1 as only and applied=0 as hide, ignoring anything else', () => {
    expect(queryFromParams(new URLSearchParams('applied=1')).applied).toBe('only')
    expect(queryFromParams(new URLSearchParams('applied=0')).applied).toBe('hide')
    expect(queryFromParams(new URLSearchParams('applied=yes')).applied).toBeUndefined()
    expect(queryFromParams(new URLSearchParams()).applied).toBeUndefined()
  })

  it('round-trips through the URL', () => {
    for (const applied of ['only', 'hide'] as const) {
      const q = queryFromParams(paramsFromQuery({ status: 'all', applied }))
      expect(q.applied).toBe(applied)
    }
    expect(paramsFromQuery({ status: 'match' }).has('applied')).toBe(false)
  })
})
