/** Central error-message extraction — single source of truth. */
export function getErrorMessage(e: unknown): string {
  if (e instanceof Error && typeof e.message === 'string' && e.message) {
    return e.message
  }
  if (typeof e === 'string' && e) {
    return e
  }
  if (e && typeof e === 'object') {
    const maybe = e as Record<string, unknown>
    // Handle { error: string } or { error: { message: string } }
    if ('error' in maybe) {
      const errVal = maybe.error
      if (typeof errVal === 'string' && errVal) return errVal
      if (errVal && typeof errVal === 'object' && typeof (errVal as Record<string, unknown>).message === 'string') {
        const m = (errVal as Record<string, unknown>).message as string
        if (m) return m
      }
      if (errVal && typeof errVal === 'object') {
        try {
          return JSON.stringify(errVal)
        } catch {
          return String(errVal)
        }
      }
    }
    if (typeof maybe.message === 'string' && maybe.message) {
      return maybe.message as string
    }
    try {
      const s = JSON.stringify(e)
      if (s && s !== '{}' && s !== '[object Object]') return s
    } catch {}
    const s = String(e)
    if (s && s !== '[object Object]') return s
  }
  try {
    const s = JSON.stringify(e)
    if (s && s !== '{}') return s
  } catch {}
  return String(e) || 'Request failed'
}
