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
  const ago = t('ago')
  if (style === 'short') {
    if (sec < 60) return t('Just now')
    if (min < 60) return `${min}m ${ago}`
    if (hr < 24) return `${hr}h ${ago}`
    if (day < 30) return `${day}d ${ago}`
    return d.toLocaleDateString()
  }
  if (sec < 60) return t('Just now')
  if (min < 60) return `${n(min, 'minute', 'minutes', 'минуту', 'минуты', 'минут')} ${ago}`
  if (hr < 24) return `${n(hr, 'hour', 'hours', 'час', 'часа', 'часов')} ${ago}`
  if (day < 30) return `${n(day, 'day', 'days', 'день', 'дня', 'дней')} ${ago}`
  if (day < 365) {
    const months = Math.floor(day / 30)
    return `${n(months, 'month', 'months', 'месяц', 'месяца', 'месяцев')} ${ago}`
  }
  const years = Math.floor(day / 365)
  return `${n(years, 'year', 'years', 'год', 'года', 'лет')} ${ago}`
}
