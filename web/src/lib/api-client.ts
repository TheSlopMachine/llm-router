import type { paths } from './generated/api-types'
import { ApiRequestError, getErrorMessage, isUsableErrorMessage, type ApiErrorCode } from './errors'

let sessionExpired: () => void = () => {}

/** The app's answer to an expired session: any request the server refuses as unauthenticated calls it. */
export function onSessionExpired(handler: () => void): void {
  sessionExpired = handler
}

/** Builds the error of a failed response from its { error, code } body. */
export async function requestError(response: Response): Promise<ApiRequestError> {
  const text = await response.text()
  let message = `HTTP ${response.status}`
  let code: ApiErrorCode | '' = ''
  try {
    const json = JSON.parse(text) as { error?: unknown; message?: unknown; code?: unknown }
    if (json && typeof json === 'object') {
      const candidate = getErrorMessage(json)
      if (isUsableErrorMessage(candidate)) message = candidate
      if (typeof json.code === 'string') code = json.code as ApiErrorCode
    }
  } catch (parseError) {
    // a body that is not JSON (a proxy's HTML page): the status line is the message
    console.error('Error response is not JSON:', parseError)
  }
  if (code === 'unauthenticated') sessionExpired()
  return new ApiRequestError(message, response.status, code)
}

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

// Extract response type for success status (200 / 201) — fallback to unknown so svelte-check surfaces real mismatches
// Swagger 2.0 uses 200 or 201 for success; 204 is no-content.
export type ApiResponse<
  P extends ApiPath,
  M extends ApiMethod<P>
> = paths[P][M] extends { responses: { 200: { schema: infer R } } }
  ? R
  : paths[P][M] extends { responses: { 201: { schema: infer R } } }
  ? R
  : paths[P][M] extends { responses: { 204: unknown } }
  ? void
  : paths[P][M] extends { responses: { 200: unknown } }
  ? unknown
  : paths[P][M] extends { responses: infer R }
  ? R extends Record<string, { schema: infer S }>
    ? S
    : unknown
  : unknown

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

  const response = await fetch(url.toString(), fetchOptions)

  if (!response.ok) throw await requestError(response)

  if (response.status === 204) {
    return null as ApiResponse<P, M>
  }

  return response.json() as Promise<ApiResponse<P, M>>
}
