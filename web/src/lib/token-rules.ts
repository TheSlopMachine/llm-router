// Token rule builders: wizard UI state in, rule payloads out.
//
// Two targets: buildBackendTokenRules adapts the wizard to the CURRENT
// backend (flat TokenRules, per-provider "use all" snapshotted to explicit
// id lists); buildDesiredTokenRules produces the structure the backend
// should accept natively (see docs/BACKEND-CHANGES.md, token rules section).

export interface WizardProviderState {
  id: string
  /** "use all" (existing and future); virtual counts as useAll when enabled */
  useAll: boolean
  /** explicitly picked credential ids of this provider */
  selected: string[]
  /** all credential ids currently known for this provider */
  known: string[]
}

export interface WizardTokenState {
  name: string
  fullAccess: boolean
  allowAllProvidersAccounts: boolean
  providers: WizardProviderState[]
  allowAllModels: boolean
  models: string[]
}

export interface BackendTokenRules {
  allowed_providers: string[] | null
  allow_all_providers: boolean
  allowed_models: string[] | null
  allow_all_models: boolean
  allowed_credentials: string[] | null
  allow_all_credentials: boolean
}

export function buildBackendTokenRules(s: WizardTokenState): BackendTokenRules {
  if (s.fullAccess) {
    return {
      allowed_providers: null,
      allow_all_providers: true,
      allowed_models: null,
      allow_all_models: true,
      allowed_credentials: null,
      allow_all_credentials: true
    }
  }
  const active = s.allowAllProvidersAccounts
    ? s.providers
    : s.providers.filter((p) => p.useAll || p.selected.length > 0)
  const creds = new Set<string>()
  if (!s.allowAllProvidersAccounts) {
    for (const p of active) {
      if (p.id === 'virtual') continue
      const ids = p.useAll ? p.known : p.selected
      for (const id of ids) creds.add(id)
    }
  }
  return {
    allowed_providers: s.allowAllProvidersAccounts ? null : active.map((p) => p.id),
    allow_all_providers: s.allowAllProvidersAccounts,
    allowed_models: s.allowAllModels ? null : [...s.models],
    allow_all_models: s.allowAllModels,
    allowed_credentials: s.allowAllProvidersAccounts ? null : [...creds],
    allow_all_credentials: s.allowAllProvidersAccounts
  }
}

export interface DesiredProviderPolicy {
  provider_id: string
  /** 'all' covers existing and future credentials; virtual carries [] */
  credentials: 'all' | string[]
}

export interface DesiredTokenRules {
  full_access: boolean
  providers: DesiredProviderPolicy[]
  models: 'all' | string[]
}

export function buildDesiredTokenRules(s: WizardTokenState): DesiredTokenRules {
  if (s.fullAccess) return { full_access: true, providers: [], models: 'all' }
  const active = s.allowAllProvidersAccounts
    ? s.providers
    : s.providers.filter((p) => p.useAll || p.selected.length > 0)
  return {
    full_access: false,
    providers: active.map((p) => ({
      provider_id: p.id,
      credentials:
        p.id === 'virtual' ? [] : s.allowAllProvidersAccounts || p.useAll ? 'all' : [...p.selected]
    })),
    models: s.allowAllModels ? 'all' : [...s.models]
  }
}
