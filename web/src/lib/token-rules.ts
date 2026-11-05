// Token rule builder: wizard UI state in, backend TokenRules payload out.
//
// Per-provider credential grants travel as credential_scopes: "use all"
// becomes {provider_id, all: true}, which also covers keys added later;
// explicit picks become {provider_id, all: false, credential_ids}.

import type { CredentialScope } from './types'

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
  allowAllProvidersCredentials: boolean
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
  credential_scopes: CredentialScope[]
}

export function buildBackendTokenRules(s: WizardTokenState): BackendTokenRules {
  if (s.fullAccess) {
    return {
      allowed_providers: null,
      allow_all_providers: true,
      allowed_models: null,
      allow_all_models: true,
      allowed_credentials: null,
      allow_all_credentials: true,
      credential_scopes: []
    }
  }
  const active = s.allowAllProvidersCredentials
    ? s.providers
    : s.providers.filter((p) => p.useAll || p.selected.length > 0)
  const scopes: CredentialScope[] = []
  if (!s.allowAllProvidersCredentials) {
    for (const p of active) {
      // Virtual carries no credentials: members gate on their own providers.
      if (p.id === 'virtual') continue
      scopes.push(p.useAll ? { provider_id: p.id, all: true } : { provider_id: p.id, all: false, credential_ids: [...p.selected] })
    }
  }
  return {
    allowed_providers: s.allowAllProvidersCredentials ? null : active.map((p) => p.id),
    allow_all_providers: s.allowAllProvidersCredentials,
    allowed_models: s.allowAllModels ? null : [...s.models],
    allow_all_models: s.allowAllModels,
    allowed_credentials: s.allowAllProvidersCredentials ? null : [],
    allow_all_credentials: s.allowAllProvidersCredentials,
    credential_scopes: scopes
  }
}
