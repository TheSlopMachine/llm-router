// Generic filtering. Replaces hand-rolled toLowerCase().includes blocks and
// the string-only filterByQuery helper.
export function filterByFields<T>(items: T[], query: string, pick: (item: T) => Array<string | undefined>): T[] {
  const q = query.trim().toLowerCase()
  if (!q) return items
  return items.filter((item) => pick(item).some((f) => (f ?? '').toLowerCase().includes(q)))
}

export function matchesAny(lower: string, needles: string[]): boolean {
  return needles.some((n) => lower.includes(n))
}
