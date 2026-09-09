import { api } from '../../lib/api'
import { modal } from '../../lib/modal.svelte'
import { getErrorMessage } from '../../lib/errors'
import type { Plugin, PluginUpdate } from '../../lib/types'
import type { PluginCardAction } from './PluginCard.svelte'
import PluginDetailsModal from './PluginDetailsModal.svelte'
import { factsFromPlugin } from './plugin-facts'
import { confirmInstall } from './install-confirm'

/**
 * Shared installed-plugin behavior for Installed and Catalog tabs.
 * Details open in a modal; install/update/reinstall confirm permissions.
 */
export function createPluginState(opts: {
  onReload: () => Promise<void>
  onError: (message: string) => void
  findUpdate: (plugin: Plugin) => PluginUpdate | null
  findRepoPath: (plugin: Plugin) => { repo_id: string; path: string } | null
}) {
  async function openDetails(plugin: Plugin): Promise<void> {
    modal.open({
      title: plugin.display_name,
      content: PluginDetailsModal,
      severity: 'medium',
      size: 'medium',
      props: { facts: factsFromPlugin(plugin), logs: [], crashes: [], loading: true }
    })
    try {
      const [logs, crashes] = await Promise.all([api.plugins.logs(plugin.id), api.plugins.crashes(plugin.id)])
      modal.updateProps({ logs, crashes, loading: false })
    } catch (e) {
      modal.close()
      opts.onError(getErrorMessage(e))
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
      await opts.onReload()
    } catch (e) {
      opts.onError(getErrorMessage(e))
    }
  }

  async function updatePlugin(plugin: Plugin, confirmLabel: string): Promise<void> {
    const target = opts.findUpdate(plugin) ?? opts.findRepoPath(plugin)
    if (!target) return
    const confirmed = await confirmInstall(
      `${confirmLabel} ${plugin.display_name}`,
      factsFromPlugin(plugin),
      confirmLabel
    )
    if (!confirmed) return
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
      case 'update':
        await updatePlugin(plugin, 'Update')
        break
      case 'reinstall':
        await updatePlugin(plugin, 'Reinstall')
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
    openDetails,
    setEnabled,
    rollback,
    removePlugin,
    updatePlugin,
    buildActions,
    handleAction
  }
}
