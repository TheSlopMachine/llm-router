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

/** byte counts as KB, MB or GB with one decimal */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(1)} ${units[unit]}`
}
