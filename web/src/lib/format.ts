// Display formatting helpers owned by lib, not by page markup.
export function formatDisableReason(reason: string | null | undefined): string {
  return reason ? `: ${reason}` : ''
}

export function formatLatency(ms: number | undefined): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${Math.round(ms)}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export function formatNanoLatency(nanoseconds: number): string {
  return formatLatency(nanoseconds / 1_000_000)
}
