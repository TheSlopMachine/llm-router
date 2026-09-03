// Manual frontend types (fallback when generated is empty, keep compatible with backend)
export interface TokenRules {
  allowed_providers?: string[] | null
  allow_all_providers?: boolean
  allowed_models?: string[] | null
  allow_all_models?: boolean
  allowed_credentials?: string[] | null
  allow_all_credentials?: boolean
}

export interface Token {
  id: string
  name: string
  token?: string
  token_hash?: string
  rules: TokenRules
  created_at: string
}

export type TokenCreateResponse = Token

export interface Credential {
  id: string
  provider_id: string
  provider_name: string
  label: string
  is_expired: boolean
  expires_at?: string
  updated_at: string
}

export interface ModelInfo {
  name: string
  display_name?: string
  context_window?: number
  max_tokens?: number
}

export interface MetricsOverview {
  total_requests: number
  total_errors: number
  peak_rpm: number
  peak_tpm_input: number
  peak_tpm_output: number
  peak_rpd: number
}

export interface TimeSeriesPoint {
  timestamp: string
  value: number
}

export type MetricsFilters = Record<string, unknown>
export type Stats = Record<string, unknown>
export type Status = Record<string, unknown>
export type ErrorResponse = { error: string }
export type ProviderStats = { model_count: number; credential_count: number; requests_today: number }

export interface Provider {
  id: string
  name: string
  type: string
  qualifier: string
  auth_type: string
  base_url: string
  icon_url: string
  supports_auth_flow: boolean
}

export interface AvailableModel {
  full_model_id: string
  provider_id: string
  provider_name: string
  provider_type: string
  model_name: string
  display_name: string
  context_window?: number
  max_tokens?: number
}

export interface Agent {
  id: string
  name: string
  description: string
  models: AgentModel[]
  instructions: AgentInstructions
  decision_model?: DecisionModelConfig
  max_tokens: number
  version: number
  is_draft: boolean
  created_at: string
  updated_at: string
}

export interface AgentModel {
  model_id: string
  priority: number
  description: string
  instructions: string
}

export interface AgentInstructions {
  content: string
  injection: 'beginning' | 'end'
}

export interface DecisionModelConfig {
  model_id: string
  system_prompt: string
}

export type AuthType = 'api_key' | 'oauth2' | 'custom'
export type TimeRange = 'hour' | '1d' | '7d' | '28d' | '90d' | 'month'

export interface ProviderModels {
  provider_id: string
  provider_name: string
  provider_type: string
  models: string[]
  model_info?: ModelInfo[]
  error?: string
}

export interface ModelsResponse {
  providers: ProviderModels[]
}

export interface TokenUsageInfo {
  requests: number
  last_used?: string
}

export interface ModalButton {
  label: string
  variant?: 'primary' | 'secondary' | 'danger'
  onClick: () => void | Promise<void>
  disabled?: boolean
  loading?: boolean
}

export type { ApiPath, ApiMethod, ApiResponse, ApiError, ApiRequestBody, ApiQueryParams } from './api-client'
export { apiCall } from './api-client'
