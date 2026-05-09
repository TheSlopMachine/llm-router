import { apiCall } from './api-client'

const API_BASE = '/api/llm-router'

// Helper to prepend API_BASE to paths
function path(p: string): `/api/llm-router${string}` {
  return `${API_BASE}${p}` as `/api/llm-router${string}`
}

export const api = {
  // System
  status: () => 
    apiCall('get', path('/status')),

  login: (username: string, password: string) => 
    apiCall('post', path('/login'), { body: { username, password } }),

  logout: () => 
    apiCall('post', path('/logout'), { body: {} }),

  bootstrap: (username: string, password: string) => 
    apiCall('post', path('/bootstrap'), { body: { username, password } }),

  // Providers
  providers: {
    list: () => 
      apiCall('get', path('/dashboard/providers')),

    adapterTypes: () => 
      apiCall('get', path('/dashboard/adapter-types')),

    stats: () => 
      apiCall('get', path('/dashboard/providers/stats')),
  },

  // Tokens
  tokens: {
    list: () => 
      apiCall('get', path('/dashboard/tokens')),

    create: (payload: { name: string; rules: { allowed_models: string[] | null } }) => 
      apiCall('post', path('/dashboard/tokens'), { 
        body: payload
      }),

    update: (id: string, payload: { name: string; rules: { allowed_models: string[] | null } }) => 
      apiCall('put', path(`/dashboard/tokens/${id}` as '/dashboard/tokens/{id}'), { 
        body: payload
      }),

    delete: (id: string) => 
      apiCall('delete', path(`/dashboard/tokens/${id}` as '/dashboard/tokens/{id}')),

    usage: () => 
      apiCall('get', path('/dashboard/tokens/usage')),
  },

  // Credentials
  credentials: {
    list: () => 
      apiCall('get', path('/dashboard/credentials')),

    delete: (id: string) => 
      apiCall('delete', path(`/dashboard/credentials/${id}` as '/dashboard/credentials/{id}')),
  },

  // Models
  models: {
    list: (providerIds: string[]) => 
      apiCall('get', path('/dashboard/models'), { 
        query: { provider_ids: providerIds } 
      }),
  },

  // Agents
  agents: {
    list: () => 
      apiCall('get', path('/dashboard/agents')),

    create: (payload: any) => 
      apiCall('post', path('/dashboard/agents'), { body: payload }),

    get: (id: string) => 
      apiCall('get', path(`/dashboard/agents/${id}` as '/dashboard/agents/{id}')),

    update: (id: string, payload: any) => 
      apiCall('put', path(`/dashboard/agents/${id}` as '/dashboard/agents/{id}'), { body: payload }),

    delete: (id: string) => 
      apiCall('delete', path(`/dashboard/agents/${id}` as '/dashboard/agents/{id}')),

    availableModels: () => 
      apiCall('get', path('/dashboard/agents/available-models')),
  },

  // Metrics
  metrics: {
    overview: (filters: { provider_id?: string; model?: string; time_range?: string }) => 
      apiCall('get', path('/dashboard/metrics/overview'), { query: filters }),

    timeSeries: (metric: string, filters: { provider_id?: string; model?: string; time_range?: string }) => 
      apiCall('get', path('/dashboard/metrics/timeseries'), { 
        query: { metric, ...filters } 
      }),

    models: () => 
      apiCall('get', path('/dashboard/metrics/models')),
  },
}
