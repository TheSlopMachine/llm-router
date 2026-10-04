// Credential/provider disablement predicates. Single owner for the
// `disabled && disabled_by === ...` chains in Providers/ProviderDetailPage.
import type { Credential, Provider } from './types'

export function isSystemDisabled(e: { disabled?: boolean; disabled_by?: string | null }): boolean {
  return !!e.disabled && e.disabled_by === 'system'
}

export function isAutoDisabled(cred: Credential): boolean {
  return !!cred.disabled && (cred.disabled_by === 'system' || cred.disabled_by === 'healthcheck')
}

export function isPluginDisabled(cred: Credential): boolean {
  return !!cred.disabled && cred.disabled_by === 'plugin'
}

export function formatDisableReason(reason: string | null | undefined): string {
  return reason ? `: ${reason}` : ''
}

export function isUnauthenticated(err: unknown, message = ''): boolean {
  if (typeof err === 'object' && err !== null && 'status' in err) {
    const status = (err as { status?: number }).status
    if (status === 401) return true
  }
  return message.toLowerCase().includes('unauthenticated')
}
