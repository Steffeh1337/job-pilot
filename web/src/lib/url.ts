/** Returns url only if it is a plain http(s) link, so a hostile feed can't inject javascript: URLs. */
export function safeUrl(url: string): string | undefined {
  try {
    const u = new URL(url)
    return u.protocol === 'https:' || u.protocol === 'http:' ? u.toString() : undefined
  } catch {
    return undefined
  }
}
