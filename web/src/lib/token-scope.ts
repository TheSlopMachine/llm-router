// Token wizard display + scope predicates. Single owner for the step
// subtitle/validity chains and full-access checks in TokenWizard.svelte.
import type { BackendTokenRules } from './token-rules'

export function hasFullAccess(r: BackendTokenRules | Record<string, unknown>): boolean {
  const rec = r as Record<string, unknown>
  return !!rec.allow_all_providers && !!rec.allow_all_models && !!rec.allow_all_credentials
}

export function resolveWizardSubtitle(wizardStep: number, tokenName: string, t: (s: string) => string): string {
  if (wizardStep === 4) return t('Token created successfully')
  const name = tokenName.trim()
  if (name) return name
  return `${t('Step')} ${wizardStep} ${t('of 4')}`
}

export function resolveStepValidity(
  wizardStep: number,
  flags: { step1Valid: boolean; step2Valid: boolean; step3Valid: boolean }
): boolean {
  if (wizardStep === 1) return flags.step1Valid
  if (wizardStep === 2) return flags.step2Valid
  if (wizardStep === 3) return flags.step3Valid
  return true
}

export function resolveModelId(m: {
  kind: string
  id: string
  fullId?: string
  providerId?: string
}): string {
  if (m.kind === 'virtual') return `virtual/${m.id}`
  if (m.fullId) return m.fullId
  if (m.providerId) return `${m.providerId}/${m.id}`
  return m.id
}
