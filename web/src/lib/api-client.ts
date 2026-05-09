import type { paths } from './generated/api-types'

// Extract path string type
export type ApiPath = keyof paths

// Extract HTTP methods for a given path
export type ApiMethod<P extends ApiPath> = keyof paths[P] & string

// Extract request body type
export type ApiRequestBody<
  P extends ApiPath,
  M extends ApiMethod<P>
> = paths[P][M] extends { parameters: { body: infer B } }
  ? B extends { [key: string]: infer T }
    ? T
    : never
  : never

// Extract query parameters type
export type ApiQueryParams<
  P extends ApiPath,
  M extends ApiMethod<P>
> = paths[P][M] extends { parameters: { query: infer Q } }
  ? Q
  : never

// Extract response type for 200 status
export type ApiResponse<
  P extends ApiPath,
  M extends ApiMethod<P>
> = paths[P][M] extends { responses: { 200: { schema: infer R } } }
  ? R
  : paths[P][M] extends { responses: { 204: unknown } }
  ? void
  : never

// Extract error response type
export type ApiError<
  P extends ApiPath,
  M extends ApiMethod<P>
> = paths[P][M] extends { responses: { 401: { schema: infer E } } }
  ? E
  : paths[P][M] extends { responses: { 400: { schema: infer E } } }
  ? E
  : paths[P][M] extends { responses: { 500: { schema: infer E } } }
  ? E
  : { error: string }

// Type-safe API call function
export async function apiCall<
  P extends ApiPath,
  M extends ApiMethod<P>
>(
  method: M,
  path: P,
  options?: {
    body?: ApiRequestBody<P, M>
    query?: ApiQueryParams<P, M>
  }
): Promise<ApiResponse<P, M>> {
  const url = new URL(path as string, window.location.origin)
  
  // Add query parameters
  if (options?.query) {
    Object.entries(options.query).forEach(([key, value]) => {
      if (Array.isArray(value)) {
        value.forEach(v => url.searchParams.append(key, String(v)))
      } else if (value !== undefined && value !== null) {
        url.searchParams.set(key, String(value))
      }
    })
  }

  const fetchOptions: RequestInit = {
    method: method.toUpperCase(),
    headers: { 'Content-Type': 'application/json' },
    credentials: 'same-origin',
  }

  if (options?.body) {
    fetchOptions.body = JSON.stringify(options.body)
  }

  const res = await fetch(url.toString(), fetchOptions)
  
  if (!res.ok) {
    const text = await res.text()
    let msg = `HTTP ${res.status}`
    try {
      const json = JSON.parse(text) as ApiError<P, M>
      msg = 'error' in json ? json.error : msg
    } catch {}
    throw new Error(msg)
  }

  if (res.status === 204) {
    return null as ApiResponse<P, M>
  }

  return res.json() as Promise<ApiResponse<P, M>>
}
