import type { Provider } from './types'

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
  if (lower.includes('preview') || lower.includes('beta') || lower.includes('experimental')) traits.push('Preview')
  if (lower.includes('vision') || lower.includes('image')) traits.push('Image')
  if (lower.includes('audio') || lower.includes('whisper') || lower.includes('tts')) traits.push('Audio')
  if (lower.includes('agent') || lower.includes('tool') || lower.includes('function')) traits.push('Agentic')
  return traits.slice(0, 2)
}

export function traitBadgeClass(trait: string): string {
  if (trait === 'Preview') return 'badge-yellow'
  if (trait === 'Image') return 'badge-blue'
  if (trait === 'Audio') return 'badge-green'
  if (trait === 'Agentic') return 'badge badge-blue'
  return 'badge-blue'
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
