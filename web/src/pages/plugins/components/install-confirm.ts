import { modal } from '$lib/modal.svelte'
import PluginInstallModal from './PluginInstallModal.svelte'
import PluginUpdateModal from './PluginUpdateModal.svelte'
import PluginUpdateAllModal from './PluginUpdateAllModal.svelte'
import type { PluginFacts } from './plugin-facts'

export interface UpdateAllRow {
  pluginId: string
  displayName: string
  current: string
  latest: string
  added: string[]
  removed: string[]
  escalatesToUnsafe: boolean
}

// confirmInstall shows the permission modal and resolves true on confirm.
export function confirmInstall(title: string, facts: PluginFacts, confirmLabel: string): Promise<boolean> {
  return modal.confirm({
    title,
    content: PluginInstallModal,
    props: { facts },
    size: 'small',
    confirmText: confirmLabel,
  })
}

export interface UpdateReview {
  displayName: string
  current: string
  latest: string
  newHosts: string[]
  newUnsafe: boolean
  added: string[]
  removed: string[]
  escalatesToUnsafe: boolean
}

// confirmUpdate shows the host-change review for one plugin update.
export function confirmUpdate(title: string, review: UpdateReview, confirmLabel: string): Promise<boolean> {
  return modal.confirm({
    title,
    content: PluginUpdateModal,
    props: { ...review },
    size: 'small',
    confirmText: confirmLabel,
  })
}

// confirmUpdateAll shows one combined host-change review for a bulk update.
export function confirmUpdateAll(rows: UpdateAllRow[], confirmLabel: string): Promise<boolean> {
  return modal.confirm({
    title: `Update ${rows.length} plugins`,
    content: PluginUpdateAllModal,
    props: { rows },
    size: 'medium',
    confirmText: confirmLabel,
  })
}
