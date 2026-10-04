// Plugin search + origin helpers. Single owner for the display_name/id/
// description chains in InstalledTab/CatalogTab.
import type { Plugin, StoreFile } from './types'

export function matchesStoreFile(f: StoreFile, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  return (
    (f.display_name || '').toLowerCase().includes(q) ||
    (f.path || '').toLowerCase().includes(q) ||
    (f.description || '').toLowerCase().includes(q)
  )
}

export function matchesPlugin(p: Plugin, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  return (
    (p.display_name || '').toLowerCase().includes(q) ||
    (p.id || '').toLowerCase().includes(q) ||
    (p.description || '').toLowerCase().includes(q) ||
    p.type_keys.some((k) => k.toLowerCase().includes(q))
  )
}

export function hasStoreOrigin(p: Plugin): boolean {
  return !!p.origin && !p.origin.manual && !!p.origin.repo_id && !!p.origin.path
}

export function storeKey(repoId: string, path: string): string {
  return `${repoId}::${path}`
}

export function hasPermissionChanges(diff: {
  added: unknown[]
  removed: unknown[]
  escalatesToUnsafe: boolean
}): boolean {
  return diff.added.length > 0 || diff.removed.length > 0 || diff.escalatesToUnsafe
}
