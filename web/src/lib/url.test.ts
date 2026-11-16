import { safeUrl } from './url'

describe('safeUrl', () => {
  it('allows http and https', () => {
    expect(safeUrl('https://example.com/a')).toBe('https://example.com/a')
    expect(safeUrl('http://example.com/')).toBe('http://example.com/')
  })
  it.each(['javascript:alert(1)', 'data:text/html,x', 'not a url', '', 'ftp://x.com'])('rejects %j', (u) =>
    expect(safeUrl(u)).toBeUndefined())
})
