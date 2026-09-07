import { apiCall as _apiCall } from './api-client'
import type { Provider, Token, ProviderStats, TokenUsageInfo, TimeRange, MetricsOverview, TimeSeriesPoint, AvailableModel, SchemaResponse, AuthStepResponse, Plugin, PluginRepo, StoreFile, PluginUpdate } from './types'

const apiCall = _apiCall

function toProvider(raw: Record<string, unknown>): Provider {
  const r = raw as Record<string, unknown>
  const typeKey = (r.type_key as string) ?? (r.type as string) ?? ''
  return {
    id: (r.id as string) ?? '',
    name: (r.name as string) ?? '',
    type: (r.type as string) ?? typeKey,
    type_key: typeKey,
    qualifier: (r.qualifier as string) ?? '',
    config: (r.config as Record<string, unknown>) ?? {},
    auth_type: (r.auth_type as string) ?? '',
    base_url: (r.base_url as string) ?? '',
    icon_url: (r.icon_url as string) ?? '',
    supports_auth_flow: (r.supports_auth_flow as boolean) ?? false,
  }
}

function toProviderStats(raw: Record<string, unknown>): ProviderStats {
  const r = raw as Record<string, unknown>
  return {
    model_count: (r.model_count as number) ?? 0,
    credential_count: (r.credential_count as number) ?? 0,
    requests_today: (r.requests_today as number) ?? 0,
  }
}

function toToken(raw: Record<string, unknown>): Token {
  const r = raw as Record<string, unknown>
  const rules = (r.rules as Record<string, unknown>) ?? {}
  return {
    id: (r.id as string) ?? '',
    name: (r.name as string) ?? '',
    token: r.token as string | undefined,
    token_hash: r.token_hash as string | undefined,
    rules: {
      allowed_providers: (rules.allowed_providers as string[] | null) ?? null,
      allow_all_providers: (rules.allow_all_providers as boolean) ?? false,
      allowed_models: (rules.allowed_models as string[] | null) ?? null,
      allow_all_models: (rules.allow_all_models as boolean) ?? false,
      allowed_credentials: (rules.allowed_credentials as string[] | null) ?? null,
      allow_all_credentials: (rules.allow_all_credentials as boolean) ?? false,
    },
    created_at: (r.created_at as string) ?? '',
  }
}

export const api = {
  // System
  status: async () => {
    const raw = (await apiCall('get', '/api/llm-router/status')) as unknown as Record<string, unknown>
    return {
      bootstrapped: (raw?.bootstrapped as boolean) ?? false,
      authenticated: (raw?.authenticated as boolean) ?? false,
      stats: raw?.stats as { providers?: number; tokens?: number; credentials?: number } | undefined,
    }
  },

  login: (username: string, password: string) =>
    apiCall('post', '/api/llm-router/login', { body: { username, password } as unknown as never }),

  logout: () =>
    apiCall('post', '/api/llm-router/logout'),

  bootstrap: (username: string, password: string) =>
    apiCall('post', '/api/llm-router/bootstrap', { body: { username, password } as unknown as never }),

  // Providers
  providers: {
    list: async (): Promise<Provider[]> => {
      const raw = (await apiCall('get', '/api/llm-router/dashboard/providers')) as unknown
      const arr = Array.isArray(raw) ? (raw as Record<string, unknown>[]) : []
      return arr.map(toProvider)
    },

    create: (payload: { name: string; base_url: string; icon_url?: string }) =>
      apiCall('post', '/api/llm-router/dashboard/providers', { body: payload as unknown as never }),

    createInstance: (payload: { name: string; type_key: string; qualifier?: string; config?: Record<string, unknown>; icon_url?: string }) =>
      apiCall('post', '/api/llm-router/dashboard/providers', { body: payload as unknown as never }),

    update: (id: string, payload: { name: string; base_url: string; icon_url?: string }) =>
      apiCall('put', `/api/llm-router/dashboard/providers/${id}` as '/api/llm-router/dashboard/providers/{id}', { body: payload as unknown as never } as never),

    updateInstance: (id: string, payload: { name: string; config?: Record<string, unknown>; icon_url?: string }) =>
      apiCall('put', `/api/llm-router/dashboard/providers/${id}` as '/api/llm-router/dashboard/providers/{id}', { body: payload as unknown as never } as never),

    delete: (id: string) =>
      apiCall('delete', `/api/llm-router/dashboard/providers/${id}` as '/api/llm-router/dashboard/providers/{id}'),

    adapterTypes: () =>
      apiCall('get', '/api/llm-router/dashboard/adapter-types') as Promise<string[]>,

    stats: async (): Promise<Record<string, ProviderStats>> => {
      const raw = (await apiCall('get', '/api/llm-router/dashboard/providers/stats')) as unknown as Record<string, Record<string, unknown>>
      const out: Record<string, ProviderStats> = {}
      if (raw && typeof raw === 'object') {
        for (const [k, v] of Object.entries(raw)) out[k] = toProviderStats(v as Record<string, unknown>)
      }
      return out
    },

    configSchema: (id: string): Promise<SchemaResponse> =>
      fetch(`/api/llm-router/dashboard/providers/${encodeURIComponent(id)}/config-schema`).then(assertOk),

    credentialSchema: (id: string): Promise<SchemaResponse> =>
      fetch(`/api/llm-router/dashboard/providers/${encodeURIComponent(id)}/credential-schema`).then(assertOk),

    configSchemaForType: (type_key: string): Promise<SchemaResponse> =>
      fetch(`/api/llm-router/dashboard/type-schemas?kind=config&type_key=${encodeURIComponent(type_key)}`).then(assertOk),
  },

  // Auth wizards (lua UI-tree)
  auth: {
    initiate: (provider_id: string): Promise<AuthStepResponse> =>
      postJson('/api/llm-router/dashboard/auth/initiate', { provider_id }),

    step: (payload: { provider_id: string; flow_id: string; action: string; values: Record<string, unknown> }): Promise<AuthStepResponse> =>
      postJson('/api/llm-router/dashboard/auth/step', payload),
  },

  // Installed plugins
  plugins: {
    list: (): Promise<Plugin[]> =>
      fetch('/api/llm-router/dashboard/plugins').then(assertOk),

    get: (id: string): Promise<Plugin> =>
      fetch(`/api/llm-router/dashboard/plugins/${encodeURIComponent(id)}`).then(assertOk),

    installFile: (source: string): Promise<Plugin> =>
      fetch('/api/llm-router/dashboard/plugins/install-file', {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: source,
      }).then(assertOk),

    installFromRepo: (repo_id: string, path: string): Promise<Plugin> =>
      postJson('/api/llm-router/dashboard/plugins/install-from-repo', { repo_id, path }),

    remove: (id: string): Promise<void> =>
      fetch(`/api/llm-router/dashboard/plugins/${encodeURIComponent(id)}`, { method: 'DELETE' }).then(assertOkVoid),

    enable: (id: string): Promise<Plugin> =>
      postJson(`/api/llm-router/dashboard/plugins/${encodeURIComponent(id)}/enable`, {}),

    disable: (id: string): Promise<Plugin> =>
      postJson(`/api/llm-router/dashboard/plugins/${encodeURIComponent(id)}/disable`, {}),

    rollback: (id: string): Promise<Plugin> =>
      postJson(`/api/llm-router/dashboard/plugins/${encodeURIComponent(id)}/rollback`, {}),

    logs: (id: string): Promise<Array<{ at: string; message: string }>> =>
      fetch(`/api/llm-router/dashboard/plugins/${encodeURIComponent(id)}/logs`).then(assertOk),

    crashes: (id: string): Promise<Array<{ at: string; type_key: string; cause: string }>> =>
      fetch(`/api/llm-router/dashboard/plugins/${encodeURIComponent(id)}/crashes`).then(assertOk),
  },

  // Plugin store
  repos: {
    list: (): Promise<PluginRepo[]> =>
      fetch('/api/llm-router/dashboard/plugin-repos').then(assertOk),

    addGitHub: (owner: string, repo: string): Promise<PluginRepo> =>
      postJson('/api/llm-router/dashboard/plugin-repos', { kind: 'github', owner, repo }),

    addGeneric: (index_url: string): Promise<PluginRepo> =>
      postJson('/api/llm-router/dashboard/plugin-repos', { kind: 'generic-index', index_url }),

    remove: (id: string): Promise<void> =>
      fetch(`/api/llm-router/dashboard/plugin-repos/${encodeURIComponent(id)}`, { method: 'DELETE' }).then(assertOkVoid),

    files: (id: string): Promise<StoreFile[]> =>
      fetch(`/api/llm-router/dashboard/plugin-repos/${encodeURIComponent(id)}/files`).then(assertOk),
  },

  store: {
    search: (): Promise<{ repos: Array<{ repo: PluginRepo; files: StoreFile[]; error: string }> }> =>
      fetch('/api/llm-router/dashboard/plugin-store/search').then(assertOk),

    updates: (): Promise<{ updates: PluginUpdate[] }> =>
      fetch('/api/llm-router/dashboard/plugin-store/updates').then(assertOk),
  },

  // Tokens
  tokens: {
    list: async (): Promise<Token[]> => {
      const raw = (await apiCall('get', '/api/llm-router/dashboard/tokens')) as unknown
      const arr = Array.isArray(raw) ? (raw as Record<string, unknown>[]) : []
      return arr.map(toToken)
    },

    create: (payload: { name: string; rules: { allowed_providers: string[] | null; allow_all_providers: boolean; allowed_models: string[] | null; allow_all_models: boolean; allowed_credentials: string[] | null; allow_all_credentials: boolean } }) =>
      apiCall('post', '/api/llm-router/dashboard/tokens', { body: payload as unknown as never }),

    update: (id: string, payload: { name: string; rules: { allowed_providers: string[] | null; allow_all_providers: boolean; allowed_models: string[] | null; allow_all_models: boolean; allowed_credentials: string[] | null; allow_all_credentials: boolean } }) =>
      apiCall('put', `/api/llm-router/dashboard/tokens/${id}` as '/api/llm-router/dashboard/tokens/{id}', { body: payload as unknown as never } as never),

    delete: (id: string) =>
      apiCall('delete', `/api/llm-router/dashboard/tokens/${id}` as '/api/llm-router/dashboard/tokens/{id}'),

    regenerate: (id: string) =>
      apiCall('post', `/api/llm-router/dashboard/tokens/${id}/regenerate` as '/api/llm-router/dashboard/tokens/{id}/regenerate'),

    usage: async (): Promise<Record<string, TokenUsageInfo>> => {
      const raw = (await apiCall('get', '/api/llm-router/dashboard/tokens/usage')) as unknown as Record<string, unknown>
      const out: Record<string, TokenUsageInfo> = {}
      if (raw && typeof raw === 'object') {
        for (const [k, v] of Object.entries(raw)) {
          if (typeof v === 'number') out[k] = { requests: v }
          else if (v && typeof v === 'object') {
            const rec = v as Record<string, unknown>
            out[k] = { requests: (rec.requests as number) ?? 0, last_used: rec.last_used as string | undefined }
          }
        }
      }
      return out
    },
  },

  // Credentials
  credentials: {
    list: () =>
      apiCall('get', '/api/llm-router/dashboard/credentials'),

    create: (payload: { provider_id: string; label?: string; data: Record<string, unknown> }) =>
      postJson('/api/llm-router/dashboard/credentials', payload),

    delete: (id: string) =>
      apiCall('delete', `/api/llm-router/dashboard/credentials/${id}` as '/api/llm-router/dashboard/credentials/{id}'),
  },

  // Models
  models: {
    list: (providerIds: string[]) =>
      apiCall('get', '/api/llm-router/dashboard/models', { query: { provider_ids: providerIds } as unknown as never }),

    available: async (): Promise<AvailableModel[]> => {
      const raw = (await apiCall('get', '/api/llm-router/dashboard/models/available')) as unknown
      const arr = Array.isArray(raw) ? (raw as Record<string, unknown>[]) : []
      return arr.map((r) => ({
        full_model_id: (r.full_model_id as string) ?? '',
        provider_id: (r.provider_id as string) ?? '',
        provider_name: (r.provider_name as string) ?? '',
        provider_type: (r.provider_type as string) ?? '',
        model_name: (r.model_name as string) ?? '',
        display_name: (r.display_name as string) ?? (r.model_name as string) ?? '',
        context_window: r.context_window as number | undefined,
        max_tokens: r.max_tokens as number | undefined,
      }))
    },
  },

  // Agents
  agents: {
    list: () =>
      apiCall('get', '/api/llm-router/dashboard/agents'),

    create: (payload: unknown) =>
      apiCall('post', '/api/llm-router/dashboard/agents', { body: payload as unknown as never }),

    get: (id: string) =>
      apiCall('get', `/api/llm-router/dashboard/agents/${id}` as '/api/llm-router/dashboard/agents/{id}'),

    update: (id: string, payload: unknown) =>
      apiCall('put', `/api/llm-router/dashboard/agents/${id}` as '/api/llm-router/dashboard/agents/{id}', { body: payload as unknown as never } as never),

    delete: (id: string) =>
      apiCall('delete', `/api/llm-router/dashboard/agents/${id}` as '/api/llm-router/dashboard/agents/{id}'),
  },

  // Metrics
  metrics: {
    overview: async (filters: { provider_id?: string; model?: string; time_range?: TimeRange | string }): Promise<MetricsOverview> => {
      const raw = (await apiCall('get', '/api/llm-router/dashboard/metrics/overview', { query: filters as unknown as never })) as unknown as Record<string, unknown>
      return {
        total_requests: (raw?.total_requests as number) ?? 0,
        total_errors: (raw?.total_errors as number) ?? 0,
        peak_requests: (raw?.peak_requests as number) ?? 0,
        peak_input_tokens: (raw?.peak_input_tokens as number) ?? 0,
        peak_output_tokens: (raw?.peak_output_tokens as number) ?? 0,
      }
    },

    timeSeries: async (metric: string, filters: { provider_id?: string; model?: string; time_range?: TimeRange | string }): Promise<TimeSeriesPoint[]> => {
      const raw = (await apiCall('get', '/api/llm-router/dashboard/metrics/timeseries', { query: { metric, ...filters } as unknown as never })) as unknown
      const arr = Array.isArray(raw) ? (raw as Record<string, unknown>[]) : []
      return arr.map((r) => ({
        timestamp: (r.timestamp as string) ?? '',
        value: (r.value as number) ?? 0,
      }))
    },

    models: () =>
      apiCall('get', '/api/llm-router/dashboard/metrics/models') as Promise<string[]>,
  },

  // Router configuration (instance-wide, RouterConfiguration bucket)
  config: {
    get: async (): Promise<{ is_cluster_node: boolean; disable_telemetry: boolean; max_retries: number }> => {
      const raw = (await apiCall('get', '/api/llm-router/dashboard/config' as never)) as unknown as Record<string, unknown>
      return {
        is_cluster_node: (raw?.is_cluster_node as boolean) ?? false,
        disable_telemetry: (raw?.disable_telemetry as boolean) ?? false,
        max_retries: (raw?.max_retries as number) ?? 7,
      }
    },
    update: (payload: { is_cluster_node: boolean; disable_telemetry: boolean; max_retries: number }) =>
      apiCall('put', '/api/llm-router/dashboard/config' as never, { body: payload as unknown as never } as never),
  },

  // Chat (dashboard session -> router proxy, no token)
  chat: {
    completions: (payload: { model: string; messages: Array<{ role: string; content: string }>; stream?: boolean }) =>
      apiCall('post', '/api/llm-router/dashboard/chat/completions', { body: payload as unknown as never } as never),
  },
}

async function assertOk(res: Response): Promise<any> {
  if (!res.ok) {
    throw new Error(await errorText(res))
  }
  return res.json()
}

async function assertOkVoid(res: Response): Promise<void> {
  if (!res.ok) {
    throw new Error(await errorText(res))
  }
}

async function postJson(path: string, payload: unknown): Promise<any> {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return assertOk(res)
}

async function errorText(res: Response): Promise<string> {
  const text = await res.text()
  try {
    const json = JSON.parse(text) as unknown
    if (json && typeof json === 'object' && 'error' in (json as Record<string, unknown>)) {
      const candidate = (json as Record<string, unknown>).error
      if (typeof candidate === 'string' && candidate) return candidate
    }
  } catch {}
  return `HTTP ${res.status}`
}
