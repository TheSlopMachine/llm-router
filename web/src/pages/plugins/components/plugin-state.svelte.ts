import { api } from '$lib/api'
import { toast } from '$lib/toast.svelte'
import { getErrorMessage } from '$lib/errors'
import { t } from '$lib/i18n.svelte'
import type { Plugin, PluginUpdate, StoreFile } from '$lib/types'

export interface PluginCardAction {
  id: string
  label: string
  icon?: string
  disabled?: boolean
  tint?: string
}

/**
 * Shared installed-plugin behavior for Installed and Catalog tabs.
 * Mutations report back through onPluginSaved/onPluginRemoved; no full reload.
 */
export function createPluginState(opts: {
  onPluginSaved: (plugin: Plugin) => void
  onPluginRemoved: (id: string) => void
  onUpdatesRefresh?: () => Promise<void>
  onError: (message: string) => void
  findUpdate: (plugin: Plugin) => PluginUpdate | null
  findRepoPath: (plugin: Plugin) => { repo_id: string; path: string } | null
  findStoreFile?: (plugin: Plugin) => StoreFile | null
}) {
  let pendingDelete = $state<{ plugin: Plugin; anchor?: HTMLElement } | null>(null)
  let deleting = $state(false)
  let pendingRollback = $state<{ plugin: Plugin; anchor?: HTMLElement } | null>(null)
  let rollingBack = $state(false)

  function openDelete(plugin: Plugin, anchor?: HTMLElement): void {
    if (pendingDelete?.plugin.id === plugin.id) {
      pendingDelete = null
    } else {
      pendingDelete = { plugin, anchor }
    }
  }

  async function doDelete(): Promise<void> {
    const target = pendingDelete
    if (!target || deleting) return
    deleting = true
    try {
      await api.plugins.remove(target.plugin.id)
      pendingDelete = null
      opts.onPluginRemoved(target.plugin.id)
    } catch (e) {
      opts.onError(getErrorMessage(e))
    } finally {
      deleting = false
    }
  }

  function openRollback(plugin: Plugin, anchor?: HTMLElement): void {
    if (pendingRollback?.plugin.id === plugin.id) {
      pendingRollback = null
    } else {
      pendingRollback = { plugin, anchor }
    }
  }

  async function doRollback(): Promise<void> {
    const target = pendingRollback
    if (!target || rollingBack) return
    rollingBack = true
    try {
      const rec = await api.plugins.rollback(target.plugin.id)
      pendingRollback = null
      opts.onPluginSaved(rec)
    } catch (e) {
      opts.onError(getErrorMessage(e))
    } finally {
      rollingBack = false
    }
  }

  async function updatePlugin(plugin: Plugin): Promise<void> {
    const update = opts.findUpdate(plugin)
    const target = update ?? opts.findRepoPath(plugin)
    if (!target) return
    const file = opts.findStoreFile?.(plugin) ?? null
    const latest = update?.latest || file?.version || plugin.version
    try {
      const rec = await api.plugins.installFromRepo(target.repo_id, target.path)
      toast.success(`${plugin.display_name} updated to v${latest}`)
      opts.onPluginSaved(rec)
      await opts.onUpdatesRefresh?.()
    } catch (e) {
      opts.onError(getErrorMessage(e))
    }
  }

  let updatingAll = $state(false)

  async function updateAll(plugins: Plugin[]): Promise<void> {
    if (updatingAll) return
    const pending = plugins.filter((p) => opts.findUpdate(p) !== null)
    if (pending.length === 0) return
    updatingAll = true
    const failures: string[] = []
    let succeeded = 0
    try {
      for (const plugin of pending) {
        const target = opts.findUpdate(plugin) ?? opts.findRepoPath(plugin)
        if (!target) {
          failures.push(`${plugin.display_name}: no repository origin`)
          continue
        }
        try {
          const rec = await api.plugins.installFromRepo(target.repo_id, target.path)
          opts.onPluginSaved(rec)
          succeeded++
        } catch (e) {
          failures.push(`${plugin.display_name}: ${getErrorMessage(e)}`)
        }
      }
      await opts.onUpdatesRefresh?.()
      if (failures.length === 0) {
        toast.success(`${succeeded} plugins updated`)
      } else {
        opts.onError(failures.join('\n'))
      }
    } finally {
      updatingAll = false
    }
  }

  function buildActions(plugin: Plugin): PluginCardAction[] {
    const update = opts.findUpdate(plugin)
    const origin = opts.findRepoPath(plugin)
    const actions: PluginCardAction[] = []
    if (update) {
      actions.push({ id: 'update', label: `${t('plugins.update_to')} v${update.latest}`, icon: 'upgrade' })
    } else if (origin) {
      actions.push({ id: 'reinstall', label: t('plugins.reinstall'), icon: 'refresh' })
    }
    if (plugin.origin?.manual) {
      actions.push({ id: 'update_file', label: t('plugins.update_from_file'), icon: 'upload_file' })
    }
    if (plugin.history_count > 0) {
      actions.push({ id: 'rollback', label: t('plugins.rollback_title'), icon: 'history' })
    }
    actions.push({ id: 'delete', label: t('common.actions.delete'), icon: 'delete', tint: '#dc2626' })
    return actions
  }

  async function updateFromFile(plugin: Plugin): Promise<void> {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = '.lua'
    input.onchange = async () => {
      const file = input.files?.[0]
      if (!file) return
      try {
        const text = await file.text()
        const rec = await api.plugins.installFile(text)
        if (rec.id !== plugin.id) {
          toast.success(`${rec.display_name} v${rec.version} installed as a separate plugin`)
        } else {
          toast.success(`${rec.display_name} updated to v${rec.version}`)
        }
        opts.onPluginSaved(rec)
      } catch (e) {
        opts.onError(getErrorMessage(e))
      }
    }
    input.click()
  }

  async function handleAction(plugin: Plugin, id: string, anchor?: HTMLElement): Promise<void> {
    switch (id) {
      case 'update':
        await updatePlugin(plugin)
        break
      case 'reinstall':
        await updatePlugin(plugin)
        break
      case 'update_file':
        await updateFromFile(plugin)
        break
      case 'rollback':
        openRollback(plugin, anchor)
        break
      case 'delete':
        openDelete(plugin, anchor)
        break
    }
  }

  return {
    openDelete,
    doDelete,
    openRollback,
    doRollback,
    updatePlugin,
    updateAll,
    buildActions,
    handleAction,
    get updatingAll(): boolean {
      return updatingAll
    },
    get pendingDelete(): { plugin: Plugin; anchor?: HTMLElement } | null {
      return pendingDelete
    },
    set pendingDelete(v: { plugin: Plugin; anchor?: HTMLElement } | null) {
      pendingDelete = v
    },
    get deleting(): boolean {
      return deleting
    },
    get pendingRollback(): { plugin: Plugin; anchor?: HTMLElement } | null {
      return pendingRollback
    },
    set pendingRollback(v: { plugin: Plugin; anchor?: HTMLElement } | null) {
      pendingRollback = v
    },
    get rollingBack(): boolean {
      return rollingBack
    }
  }
}
