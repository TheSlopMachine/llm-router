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
  disabled?: boolean
  // Who disabled the credential: admin action, health-check verdict,
  // plugin dead-key verdict, or legacy traffic auto-disable rows.
  disabled_by?: 'admin' | 'system' | 'healthcheck' | 'plugin' | null
  disabled_reason?: string | null
  disabled_at?: string | null
  parked?: boolean
  parked_until?: string | null
  park_reason?: string | null
  order?: number
  request_count?: number
  success_count?: number
  expires_at?: string
  updated_at: string
}

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
  disabled: boolean
  disabled_by?: 'admin' | 'system' | null
  disabled_reason?: string | null
  disabled_at?: string | null
}

export interface ModelOverride {
  provider_id: string
  name: string
  disabled?: boolean
  custom?: boolean
  display_name?: string
  capabilities?: string[]
  input_modalities?: string[]
  output_modalities?: string[]
}

export interface ProviderBundle {
  instance: Provider
  credentials: Credential[]
  overrides?: ModelOverride[]
}

export interface ModelReasoning {
  supported_efforts?: string[]
  default_effort?: string
  default_enabled?: boolean
  mandatory?: boolean
}

export interface ProviderModel {
  name: string
  display_name: string
  rpm: number
  tpm: number
  rpd: number
  context_window?: number
  max_tokens?: number
  capabilities: string[]
  reasoning?: ModelReasoning
  input_modalities?: string[]
  output_modalities?: string[]
  endpoints?: string[]
  disabled: boolean
  custom: boolean
}

export interface ProviderVMGroup {
  endpoint: string
  label: string
  models: string[]
  virtual?: VirtualModel | null
}

export interface TestResult {
  ok: boolean
  latency_ms: number
  error?: string
  response?: string
  quota_exceeded?: boolean
  code?: string
  summary?: string
  proxy?: string
}

export interface Proxy {
  id: string
  url: string
  location: string
  latency: number
  score: number
  last_checked: string
}

export interface ProxyStatus {
  total: number
  active: number
  refreshing: boolean
  last_refresh_at?: string
  last_refresh_duration: number
  next_refresh_at?: string
  refresh_interval: number
  last_error?: string
}

export interface ProxyPool {
  id: string
  name: string
  entries: Array<{ url: string; country?: string }>
}

export interface ProxySourceInfo {
  key: string
  name: string
  total: number
  unsupported: number
  last_fetch_at?: string
  last_error?: string
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
export type ProviderStats = { model_count: number; credential_count: number }

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
  disabled: boolean
  // Backend 0.3.0 auto-disable contract: absent until it lands.
  disabled_by?: 'admin' | 'system' | null
  disabled_reason?: string | null
  disabled_at?: string | null
}

export interface ProviderRetry {
  mode: 'fail_fast' | 'next_proxy'
  max_attempts: number
}

export interface AvailableModel {
  full_model_id: string
  provider_id: string
  provider_name: string
  provider_type: string
  model_name: string
  display_name: string
  description?: string
  context_window?: number
  max_tokens?: number
  capabilities?: string[]
  input_modalities?: string[]
  output_modalities?: string[]
  reasoning?: ProviderModel['reasoning']
  supported_parameters?: string[]
}

export interface VirtualModel {
  id: string
  name: string
  description: string
  models: VirtualModelEntry[]
  instruction: string
  managed_by?: string
  disabled?: boolean
  version: number
  created_at: string
  updated_at: string
}

export interface VirtualModelEntry {
  model_id: string
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
  models_filter?: string
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
  enabled?: boolean
}

export interface ProxySchemaResponse {
  nodes: UINode[] | null
  enabled: boolean
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
  origin: { repo_id: string; path: string; manual: boolean }
  installed_at: string
  updated_at: string
  history_count: number
}

export interface PluginRepo {
  id: string
  url: string
  title: string
  description: string
  builtin: boolean
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

export interface SubsystemStats {
  providers: number
  credentials: number
  virtual_models: number
  plugins: number
  plugin_repos: number
  tokens: number
}

export type DoctorCategory =
  | 'orphan_credentials'
  | 'orphan_model_overrides'
  | 'orphan_model_infos'
  | 'orphan_geo_bans'
  | 'orphan_virtual_models'
  | 'duplicate_virtual_models'
  | 'orphan_plugin_storage'
  | 'corrupt_plugin_storage'
  | 'missing_provider_backend'
  | 'broken_token_indexes'

export interface DoctorIssue {
  category: DoctorCategory
  title: string
  description: string
  count: number
  keys?: string[]
}

export interface DoctorReport {
  issues: DoctorIssue[]
  total_issues: number
}
