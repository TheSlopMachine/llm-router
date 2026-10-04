// Set diff for @allow_host permission review on plugin updates.
export interface PermissionDiff {
  added: string[]
  removed: string[]
  escalatesToUnsafe: boolean
  hasChanges: boolean
}

export function diffAllowHosts(
  oldHosts: string[],
  oldUnsafe: boolean,
  newHosts: string[],
  newUnsafe: boolean
): PermissionDiff {
  const oldSet = new Set(oldHosts ?? [])
  const newSet = new Set(newHosts ?? [])
  const added = [...newSet].filter((h) => !oldSet.has(h))
  const removed = [...oldSet].filter((h) => !newSet.has(h))
  const escalatesToUnsafe = !oldUnsafe && newUnsafe
  return { added, removed, escalatesToUnsafe, hasChanges: added.length > 0 || removed.length > 0 || escalatesToUnsafe }
}
