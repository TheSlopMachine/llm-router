import { language } from '../language.svelte'
import { en } from './en'
import { ru } from './ru'
import type { TranslationKey } from './types'

function resolveLang(): 'en' | 'ru' {
  const v = language.value
  if (v === 'ru') return 'ru'
  if (v === 'en') return 'en'
  if (typeof navigator !== 'undefined' && navigator.language.toLowerCase().startsWith('ru')) return 'ru'
  return 'en'
}

/** Resolved UI language code for Intl-based widgets. */
export function locale(): 'en' | 'ru' {
  return resolveLang()
}

export function t(key: TranslationKey): string {
  const dict = resolveLang() === 'ru' ? ru : en
  return dict[key] ?? key
}

const backendSummaryMap: Record<string, TranslationKey> = {
  'probe failed': 'credentials.probe_failed',
  'authentication failed': 'credentials.auth_failed',
  'invalid request': 'credentials.invalid_request',
  'quota exceeded, temporary': 'credentials.quota_temporary',
  'region blocked': 'credentials.region_blocked',
  'request timed out': 'credentials.timeout',
  'upstream error': 'credentials.upstream_error',
  'connection failed': 'credentials.summary.connection_failed',
  'payment required': 'credentials.summary.payment_required',
  'backend overloaded': 'credentials.summary.backend_overloaded',
  'health check not supported': 'credentials.summary.health_unsupported',
  'health check failed': 'credentials.summary.health_failed',
  'credential unhealthy': 'credentials.summary.unhealthy',
  'credential unhealthy, disabled': 'credentials.summary.unhealthy_disabled',
  'health check inconclusive': 'credentials.summary.inconclusive',
  'disable failed': 'credentials.summary.disable_failed',
  'is healthy': 'credentials.status.healthy',
  'failed': 'credentials.status.failed',
  'refreshed': 'credentials.status.refreshed'
}

export function tb(summary: string): string {
  const key = backendSummaryMap[summary]
  if (!key) return summary
  return t(key)
}

export function n(count: number, keyOne: TranslationKey, keyMany: TranslationKey): string {
  const isRussian = resolveLang() === 'ru'
  
  if (isRussian) {
    const m10 = count % 10
    const m100 = count % 100
    
    const baseKey = keyOne.replace(/\.one$/, '')
    let form: TranslationKey
    
    if (m10 === 1 && m100 !== 11) {
      form = `${baseKey}.one` as TranslationKey
    } else if ([2, 3, 4].includes(m10) && ![12, 13, 14].includes(m100)) {
      form = `${baseKey}.few` as TranslationKey
    } else {
      form = `${baseKey}.many` as TranslationKey
    }
    
    return `${count} ${ru[form] ?? keyOne}`
  }
  
  return `${count} ${count === 1 ? en[keyOne] : en[keyMany]}`
}

export { language }
export type { TranslationKey } from './types'
export type { TranslationRecord } from './types'
