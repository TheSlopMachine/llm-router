// Pure logic behind the Logs tab: query encoding, buffer merging and facets.
// No fetching and no DOM here; the page owns both.
import type { Tint } from '@fuckyouryan/fui'
import type { TranslationKey } from './i18n/types'
import type { EventEntry, EventKeyFilter, EventsQuery } from './types'

export const SEVERITY_LEVELS: ReadonlyArray<{ level: number; tint: Tint; label: TranslationKey }> = [
  { level: 0, tint: 'neutral', label: 'metrics.logs.severity_debug.label' },
  { level: 1, tint: 'info', label: 'metrics.logs.severity_info.label' },
  { level: 2, tint: 'warning', label: 'metrics.logs.severity_warn.label' },
  { level: 3, tint: 'danger', label: 'metrics.logs.severity_error.label' },
  { level: 4, tint: 'purple', label: 'metrics.logs.severity_fatal.label' },
]

export const TIME_RANGES: ReadonlyArray<{ id: string; minutes: number; label: TranslationKey }> = [
  { id: '15m', minutes: 15, label: 'metrics.logs.range_15m.label' },
  { id: '1h', minutes: 60, label: 'metrics.logs.range_1h.label' },
  { id: '6h', minutes: 360, label: 'metrics.logs.range_6h.label' },
  { id: '24h', minutes: 1440, label: 'metrics.logs.range_24h.label' },
  { id: '3d', minutes: 4320, label: 'metrics.logs.range_3d.label' },
]

// Keys the services log (docs/EVENT-KEYS.md); keys seen in loaded events are added on top.
export const KNOWN_KEYS = [
  'subsystem', 'provider_id', 'type', 'model', 'endpoint', 'token_id', 'code', 'status',
  'operation', 'cred_id', 'plugin', 'event', 'source', 'lane', 'pool', 'reason',
] as const

// Keys worth a facet: few distinct values, each one a sensible filter.
export const FACET_KEYS = ['subsystem', 'provider_id', 'model', 'endpoint', 'code', 'status', 'operation'] as const

export function severityTint(level: number): Tint {
  return SEVERITY_LEVELS[Math.min(Math.max(level, 0), SEVERITY_LEVELS.length - 1)].tint
}

export function severityLabel(level: number): TranslationKey {
  return SEVERITY_LEVELS[Math.min(Math.max(level, 0), SEVERITY_LEVELS.length - 1)].label
}

// The API reads one predicate per prefix and key, so the wire format is a flat record.
export function eventsQueryParams(query: EventsQuery): Record<string, string> {
  const params: Record<string, string> = {}
  if (query.severity !== undefined && query.severity > 0) params.severity = String(query.severity)
  if (query.start) params.start = query.start
  if (query.end) params.end = query.end
  if (query.limit !== undefined) params.limit = String(query.limit)
  for (const filter of query.filters) {
    params[`${filter.exclude ? 'exclude' : 'filter'}.${filter.key}`] = filter.value
  }
  return params
}

// A new predicate replaces the one on the same key and side.
export function withFilter(filters: EventKeyFilter[], next: EventKeyFilter): EventKeyFilter[] {
  const rest = filters.filter((filter) => !(filter.key === next.key && filter.exclude === next.exclude))
  return [...rest, next]
}

export function withoutFilter(filters: EventKeyFilter[], target: EventKeyFilter): EventKeyFilter[] {
  return filters.filter((filter) => !(filter.key === target.key && filter.exclude === target.exclude))
}

// Merges by id, sorted oldest first; past `cap` the oldest entries are dropped.
export function mergeEntries(current: EventEntry[], incoming: EventEntry[], cap: number): EventEntry[] {
  const byId = new Map<number, EventEntry>()
  for (const entry of current) byId.set(entry.id, entry)
  for (const entry of incoming) byId.set(entry.id, entry)
  const merged = [...byId.values()].sort((left, right) => left.id - right.id)
  return merged.length > cap ? merged.slice(merged.length - cap) : merged
}

// undefined: key absent. null: key present without a value.
export function keyValue(entry: EventEntry, key: string): string | null | undefined {
  const found = entry.keys.find((candidate) => candidate.key === key)
  return found === undefined ? undefined : found.value
}

export function matchesSearch(entry: EventEntry, needle: string): boolean {
  if (needle === '') return true
  const lowered = needle.toLowerCase()
  if (entry.message.toLowerCase().includes(lowered)) return true
  return entry.keys.some(
    (pair) => pair.key.toLowerCase().includes(lowered) || (pair.value ?? '').toLowerCase().includes(lowered),
  )
}

export interface FacetValue {
  value: string
  count: number
}

export interface FacetGroup {
  key: string
  values: FacetValue[]
}

export function facetGroups(entries: EventEntry[], keys: ReadonlyArray<string>, perKey: number): FacetGroup[] {
  const counts = new Map<string, Map<string, number>>(keys.map((key) => [key, new Map()]))
  for (const entry of entries) {
    for (const pair of entry.keys) {
      const bucket = counts.get(pair.key)
      if (bucket === undefined || pair.value === null || pair.value === '') continue
      bucket.set(pair.value, (bucket.get(pair.value) ?? 0) + 1)
    }
  }
  return keys
    .map((key) => ({
      key,
      values: [...(counts.get(key) ?? [])]
        .map(([value, count]) => ({ value, count }))
        .sort((left, right) => right.count - left.count || left.value.localeCompare(right.value))
        .slice(0, perKey),
    }))
    .filter((group) => group.values.length > 0)
}

// Values a key takes in the loaded entries, most frequent first.
export function observedValues(entries: EventEntry[], key: string, limit: number): string[] {
  const counts = new Map<string, number>()
  for (const entry of entries) {
    for (const pair of entry.keys) {
      if (pair.key === key && pair.value !== null && pair.value !== '') {
        counts.set(pair.value, (counts.get(pair.value) ?? 0) + 1)
      }
    }
  }
  return [...counts]
    .sort((left, right) => right[1] - left[1] || left[0].localeCompare(right[0]))
    .slice(0, limit)
    .map(([value]) => value)
}

// Known keys first, then keys seen in the loaded entries, most frequent first.
export function selectableKeys(entries: EventEntry[]): string[] {
  const seen = new Map<string, number>()
  for (const entry of entries) {
    for (const pair of entry.keys) seen.set(pair.key, (seen.get(pair.key) ?? 0) + 1)
  }
  const extra = [...seen]
    .filter(([key]) => !(KNOWN_KEYS as ReadonlyArray<string>).includes(key))
    .sort((left, right) => right[1] - left[1] || left[0].localeCompare(right[0]))
    .map(([key]) => key)
  return [...KNOWN_KEYS, ...extra]
}

const pad = (value: number): string => String(value).padStart(2, '0')

// <input type="datetime-local"> value for a moment, in the browser's time zone.
export function toDateTimeLocal(moment: number): string {
  const date = new Date(moment)
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

export interface RangeBounds {
  // null: unbounded on that side
  start: number | null
  end: number | null
}

export const CUSTOM_RANGE = 'custom'

export function rangeBounds(rangeId: string, customStart: string, customEnd: string, now: number): RangeBounds {
  if (rangeId === CUSTOM_RANGE) {
    const start = customStart === '' ? Number.NaN : new Date(customStart).getTime()
    const end = customEnd === '' ? Number.NaN : new Date(customEnd).getTime()
    return { start: Number.isNaN(start) ? null : start, end: Number.isNaN(end) ? null : end }
  }
  const preset = TIME_RANGES.find((range) => range.id === rangeId)
  return { start: preset === undefined ? null : now - preset.minutes * 60_000, end: null }
}

export function isoOrUndefined(moment: number | null): string | undefined {
  return moment === null ? undefined : new Date(moment).toISOString()
}
