export type RelativeStyle = 'long' | 'short'

export function formatRelativeTime(iso: string | undefined, style: RelativeStyle = 'long'): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return '—'
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const sec = Math.floor(diffMs / 1000)
  const min = Math.floor(sec / 60)
  const hr = Math.floor(min / 60)
  const day = Math.floor(hr / 24)
  if (style === 'short') {
    if (sec < 60) return 'Just now'
    if (min < 60) return `${min}m ago`
    if (hr < 24) return `${hr}h ago`
    if (day < 30) return `${day}d ago`
    return d.toLocaleDateString()
  }
  if (sec < 60) return 'Just now'
  if (min < 60) return `${min} minute${min !== 1 ? 's' : ''} ago`
  if (hr < 24) return `${hr} hour${hr !== 1 ? 's' : ''} ago`
  if (day < 30) return `${day} day${day !== 1 ? 's' : ''} ago`
  if (day < 365) {
    const months = Math.floor(day / 30)
    return `${months} month${months !== 1 ? 's' : ''} ago`
  }
  const years = Math.floor(day / 365)
  return `${years} year${years !== 1 ? 's' : ''} ago`
}
