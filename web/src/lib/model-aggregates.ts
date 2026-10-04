// Virtual-model member aggregation. Single owner for the union/min idiom
// repeated across VirtualModels, Models, TokenWizard, VirtualModelEditPage.
export function unionMembers<T>(members: Array<{ model_id: string } | string>, pick: (id: string) => T[] | undefined): T[] {
  const out: T[] = []
  for (const m of members) {
    const id = typeof m === 'string' ? m : m.model_id
    for (const x of pick(id) ?? []) {
      if (!out.includes(x)) out.push(x)
    }
  }
  return out
}

export function minPositive(values: Array<number | undefined>): number {
  let min = 0
  for (const v of values) {
    if (v && v > 0 && (min === 0 || v < min)) min = v
  }
  return min
}

export function intersectMembers<T>(members: string[], pick: (id: string) => T[] | undefined): T[] {
  const lists = members.map((id) => pick(id) ?? []).filter((l) => l.length > 0)
  if (lists.length === 0) return []
  const [first, ...rest] = lists
  return first.filter((x) => rest.every((l) => l.includes(x)))
}
