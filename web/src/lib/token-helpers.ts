import type { Provider } from './types'
import { matchesAny } from './filter'

export function providerDescription(p: Provider): string {
  if (p.auth_type === 'api_key') return 'API key access'
  if (p.auth_type === 'oauth2') return 'OAuth access'
  if (p.auth_type === 'custom') return 'Custom access'
  if (p.auth_type) return `${p.auth_type} access`
  return `${p.type} access`
}

export function modelTraits(model: string): string[] {
  const lower = model.toLowerCase()
  const traits: string[] = []
  if (matchesAny(lower, ['preview', 'beta', 'experimental'])) traits.push('Preview')
  if (lower.includes('vision') || lower.includes('image')) traits.push('Image')
  if (matchesAny(lower, ['audio', 'whisper', 'tts'])) traits.push('Audio')
  if (matchesAny(lower, ['agent', 'tool', 'function'])) traits.push('Agentic')
  return traits.slice(0, 2)
}

export function traitBadgeClass(trait: string): string {
  if (trait === 'Preview') return 'chip-yellow'
  if (trait === 'Image') return 'chip-blue'
  if (trait === 'Audio') return 'chip-green'
  if (trait === 'Agentic') return 'chip chip-blue'
  return 'chip-blue'
}

export function toggleSet<T>(set: Set<T>, value: T): Set<T> {
  const next = new Set(set)
  if (next.has(value)) next.delete(value)
  else next.add(value)
  return next
}

export function filterByQuery(items: string[], query: string): string[] {
  if (!query.trim()) return items
  const q = query.toLowerCase()
  return items.filter((m) => m.toLowerCase().includes(q))
}
