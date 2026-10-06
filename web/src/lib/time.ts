import { n, t } from './i18n.svelte'

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
  const ago = t('common.time.ago')
  if (style === 'short') {
    if (sec < 60) return t('common.time.just_now')
    if (min < 60) return `${min}m ${ago}`
    if (hr < 24) return `${hr}h ${ago}`
    if (day < 30) return `${day}d ${ago}`
    return d.toLocaleDateString()
  }
  if (sec < 60) return t('common.time.just_now')
  if (min < 60) return `${n(min, 'time.units.minute.one', 'time.units.minute.many')} ${ago}`
  if (hr < 24) return `${n(hr, 'time.units.hour.one', 'time.units.hour.many')} ${ago}`
  if (day < 30) return `${n(day, 'time.units.day.one', 'time.units.day.many')} ${ago}`
  if (day < 365) {
    const months = Math.floor(day / 30)
    return `${n(months, 'time.units.month.one', 'time.units.month.many')} ${ago}`
  }
  const years = Math.floor(day / 365)
  return `${n(years, 'time.units.year.one', 'time.units.year.many')} ${ago}`
}
