import { api } from '../../lib/api'
import { modal } from '../../lib/modal.svelte'
import { getErrorMessage } from '../../lib/errors'
import type { Plugin, PluginUpdate } from '../../lib/types'
import type { PluginCardAction } from './PluginCard.svelte'

interface PluginDetails {
  logs: Array<{ at: string; message: string }>
  crashes: Array<{ at: string; type_key: string; cause: string }>
}

/**
 * Shared installed-plugin behavior for Installed and Catalog tabs.
 * Owns expanded details, lazy logs/crashes loading and all mutations.
 */
export function createPluginState(opts: {
  onReload: () => Promise<void>
  onError: (message: string) => void
  findUpdate: (plugin: Plugin) => PluginUpdate | null
  findRepoPath: (plugin: Plugin) => { repo_id: string; path: string } | null
}) {
  let expanded = $state<string | null>(null)
  let details = $state<Record<string, PluginDetails>>({})
  let detailsLoading = $state<Record<string, boolean>>({})

  async function toggleDetails(plugin: Plugin): Promise<void> {
    if (expanded === plugin.id) {
      expanded = null
      return
    }
    expanded = plugin.id
    if (!details[plugin.id]) {
      detailsLoading[plugin.id] = true
      try {
        const [logs, crashes] = await Promise.all([api.plugins.logs(plugin.id), api.plugins.crashes(plugin.id)])
        details[plugin.id] = { logs, crashes }
      } catch (e) {
        opts.onError(getErrorMessage(e))
      } finally {
        detailsLoading[plugin.id] = false
      }
    }
  }

  async function setEnabled(plugin: Plugin, enabled: boolean): Promise<void> {
    try {
      if (enabled) {
        await api.plugins.enable(plugin.id)
      } else {
        await api.plugins.disable(plugin.id)
      }
      await opts.onReload()
    } catch (e) {
      opts.onError(getErrorMessage(e))
    }
  }

  async function rollback(plugin: Plugin): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Roll back plugin',
      message: `Roll "${plugin.display_name}" back to the previous version?`,
      severity: 'medium',
      confirmText: 'Roll back',
      cancelText: 'Cancel',
      danger: false
    })
    if (!confirmed) return
    try {
      await api.plugins.rollback(plugin.id)
      await opts.onReload()
    } catch (e) {
      opts.onError(getErrorMessage(e))
    }
  }

  async function removePlugin(plugin: Plugin): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Delete plugin',
      message: `Delete "${plugin.display_name}"? Providers using its types will stop working.`,
      severity: 'high',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true
    })
    if (!confirmed) return
    try {
      await api.plugins.remove(plugin.id)
      if (expanded === plugin.id) expanded = null
      await opts.onReload()
    } catch (e) {
      opts.onError(getErrorMessage(e))
    }
  }

  async function updatePlugin(plugin: Plugin): Promise<void> {
    const target = opts.findUpdate(plugin) ?? opts.findRepoPath(plugin)
    if (!target) return
    try {
      await api.plugins.installFromRepo(target.repo_id, target.path)
      await opts.onReload()
    } catch (e) {
      opts.onError(getErrorMessage(e))
    }
  }

  function buildActions(plugin: Plugin): PluginCardAction[] {
    const update = opts.findUpdate(plugin)
    const origin = opts.findRepoPath(plugin)
    const actions: PluginCardAction[] = []
    if (plugin.enabled) {
      actions.push({ id: 'disable', label: 'Disable', icon: 'toggle_off' })
    } else {
      actions.push({ id: 'enable', label: 'Enable', icon: 'toggle_on' })
    }
    actions.push({ id: 'details', label: expanded === plugin.id ? 'Hide details' : 'Details', icon: 'info' })
    if (update) {
      actions.push({ id: 'update', label: `Update to v${update.latest}`, icon: 'upgrade' })
    } else if (origin) {
      actions.push({ id: 'reinstall', label: 'Reinstall', icon: 'refresh' })
    }
    if (plugin.history_count > 0) {
      actions.push({ id: 'rollback', label: 'Roll back', icon: 'history' })
    }
    actions.push({ id: 'delete', label: 'Delete', icon: 'delete', danger: true })
    return actions
  }

  async function handleAction(plugin: Plugin, id: string): Promise<void> {
    switch (id) {
      case 'enable':
        await setEnabled(plugin, true)
        break
      case 'disable':
        await setEnabled(plugin, false)
        break
      case 'details':
        await toggleDetails(plugin)
        break
      case 'update':
        await updatePlugin(plugin)
        break
      case 'reinstall':
        await updatePlugin(plugin)
        break
      case 'rollback':
        await rollback(plugin)
        break
      case 'delete':
        await removePlugin(plugin)
        break
    }
  }

  return {
    get expanded(): string | null {
      return expanded
    },
    get details(): Record<string, PluginDetails> {
      return details
    },
    get detailsLoading(): Record<string, boolean> {
      return detailsLoading
    },
    toggleDetails,
    setEnabled,
    rollback,
    removePlugin,
    updatePlugin,
    buildActions,
    handleAction
  }
}
