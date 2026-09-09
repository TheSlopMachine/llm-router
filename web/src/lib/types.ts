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
  peak_requests: number
  peak_input_tokens: number
  peak_output_tokens: number
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
  type_key: string
  qualifier: string
  config: Record<string, unknown>
  auth_type: string
  base_url: string
  icon_url: string
  supports_auth_flow: boolean
  is_ui_readonly: boolean
  is_ui_hidden: boolean
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

export interface RouterConfiguration {
  is_cluster_node: boolean
  disable_telemetry: boolean
  max_retries: number
}

export interface UINode {
  type: 'text' | 'input' | 'select' | 'checkbox' | 'button' | 'link' | 'banner' | 'group' | 'flow' | 'grid' | 'section' | 'spacer' | 'divider' | 'secret' | 'code'
  text?: string
  name?: string
  label?: string
  input_type?: string
  required?: boolean
  options?: string[]
  url?: string
  variant?: string
  form_action?: string
  content?: UINode[]
  placeholder?: string
  value?: unknown
  direction?: string
  align?: string
  justify?: string
  gap?: string
  columns?: number
  title?: string
  subtitle?: string
  wrap?: boolean
  grow?: boolean
  size?: string
  option_labels?: Record<string, string>
}

export interface SchemaResponse {
  nodes: UINode[] | null
  fallback?: string
}

export type AuthStepStatus = 'render' | 'redirect' | 'complete'

export interface AuthStepResponse {
  status: AuthStepStatus
  nodes?: UINode[]
  redirect_url?: string
  flow_id?: string
  provider_id?: string
  message?: string
  credential_id?: string
}

export interface Plugin {
  id: string
  display_name: string
  author: string
  version: string
  router_version: string
  description: string
  license: string
  allow_hosts: string[]
  unsafe: boolean
  type_keys: string[]
  enabled: boolean
  origin: { repo_id: string; path: string; manual: boolean }
  installed_at: string
  updated_at: string
  history_count: number
}

export interface PluginRepo {
  id: string
  kind: string
  owner: string
  repo: string
  index_url: string
}

export interface StoreFile {
  repo_id: string
  path: string
  display_name: string
  author: string
  version: string
  description: string
  allow_hosts: string[]
  unsafe: boolean
  installed: boolean
  installed_version: string
  update_available: boolean
  error?: string
}

export interface PluginUpdate {
  plugin_id: string
  current: string
  latest: string
  repo_id: string
  path: string
  update_available: boolean
  error?: string
}

export type { ApiPath, ApiMethod, ApiResponse, ApiError, ApiRequestBody, ApiQueryParams } from './api-client'
export { apiCall } from './api-client'
export type { ModalButton, ModalMenu, ModalMenuAction } from './modal.svelte'
