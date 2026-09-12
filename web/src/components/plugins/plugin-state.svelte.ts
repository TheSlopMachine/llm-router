import { api } from '../../lib/api'
import { modal } from '../../lib/modal.svelte'
import { toast } from '../../lib/toast.svelte'
import { getErrorMessage } from '../../lib/errors'
import { t } from '../../lib/i18n.svelte'
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

  async function rollback(plugin: Plugin): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('Roll back plugin'),
      message: `${t('Roll')} "${plugin.display_name}" ${t('back to the previous version?')}`,
      severity: 'medium',
      confirmText: t('Roll back'),
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
      title: t('Delete plugin'),
      message: `${t('Delete')} "${plugin.display_name}"? ${t('Providers using its types will stop working.')}`,
      severity: 'high',
      confirmText: t('Delete'),
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
      actions.push({ id: 'update', label: `${t('Update to')} v${update.latest}`, icon: 'upgrade' })
    } else if (origin) {
      actions.push({ id: 'reinstall', label: t('Reinstall'), icon: 'refresh' })
    }
    if (plugin.origin?.manual) {
      actions.push({ id: 'update_file', label: t('Update from file…'), icon: 'upload_file' })
    }
    if (plugin.history_count > 0) {
      actions.push({ id: 'rollback', label: t('Roll back'), icon: 'history' })
    }
    actions.push({ id: 'delete', label: t('Delete'), icon: 'delete', danger: true })
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
        await opts.onReload()
      } catch (e) {
        opts.onError(getErrorMessage(e))
      }
    }
    input.click()
  }

  async function handleAction(plugin: Plugin, id: string): Promise<void> {
    switch (id) {
      case 'update':
        await updatePlugin(plugin, 'Update')
        break
      case 'reinstall':
        await updatePlugin(plugin, 'Reinstall')
        break
      case 'update_file':
        await updateFromFile(plugin)
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
    rollback,
    removePlugin,
    updatePlugin,
    buildActions,
    handleAction
  }
}
