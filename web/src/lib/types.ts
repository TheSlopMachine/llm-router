// Re-export generated backend types
import type { components } from './generated/api-types'

// Backend API types (auto-generated from Swagger)
export type Provider = components['schemas']['Provider']
export type Token = components['schemas']['Token']
export type TokenRules = components['schemas']['TokenRules']
export type TokenCreateResponse = components['schemas']['TokenCreateResponse']
export type Credential = components['schemas']['Credential']
export type ModelInfo = components['schemas']['ModelInfo']
export type MetricsOverview = components['schemas']['MetricsOverview']
export type TimeSeriesPoint = components['schemas']['TimeSeriesPoint']
export type MetricsFilters = components['schemas']['MetricsFilters']
export type Stats = components['schemas']['Stats']
export type Status = components['schemas']['Status']
export type ErrorResponse = components['schemas']['ErrorResponse']
export type ProviderStats = components['schemas']['ProviderStats']

// Agent types (manually defined until Swagger regeneration)
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

// Type aliases for convenience
export type AuthType = 'api_key' | 'oauth2' | 'custom'
export type TimeRange = 'hour' | '1d' | '7d' | '28d' | '90d' | 'month'

// Derived types for API responses
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

// Frontend-only types (not in backend API)
export interface ModalButton {
  label: string
  variant?: 'primary' | 'secondary' | 'danger'
  onClick: () => void | Promise<void>
  disabled?: boolean
  loading?: boolean
}

// Re-export API client utilities
export type { ApiPath, ApiMethod, ApiResponse, ApiError, ApiRequestBody, ApiQueryParams } from './api-client'
export { apiCall } from './api-client'
