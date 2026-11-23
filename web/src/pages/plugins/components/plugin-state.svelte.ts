import { api } from '$lib/api'
import { modal, toast } from '@fuckyouryan/fui'
import DockerWizard from '../../../components/domain/DockerWizard.svelte'
import { getErrorMessage } from '$lib/errors'
import { t, tx } from '$lib/i18n.svelte'
import type { Plugin, PluginPreview, PluginUpdate, StoreFile } from '$lib/types'

/** an uploaded file waiting for the operator to approve its permissions */
export interface PendingFile {
  source: string
  preview: PluginPreview
  /** the installed plugin with the same id, compared against */
  current: Plugin | null
  /** the plugin the operator chose Update From File on */
  target: Plugin | null
  anchor?: HTMLElement
}

/**
 * Shared installed-plugin behavior for Installed and Catalog tabs.
 * Mutations report back through onPluginSaved/onPluginRemoved; no full reload.
 */
export function createPluginState(options: {
  onPluginSaved: (plugin: Plugin) => void
  onPluginRemoved: (id: string) => void
  onUpdatesRefresh?: () => Promise<void>
  onError: (message: string) => void
  findUpdate: (plugin: Plugin) => PluginUpdate | null
  findRepoPath: (plugin: Plugin) => { repo_id: string; path: string } | null
  findStoreFile?: (plugin: Plugin) => StoreFile | null
  findPlugin?: (id: string) => Plugin | null
}) {
  let pendingDelete = $state<{ plugin: Plugin; anchor?: HTMLElement } | null>(null)
  let deleting = $state(false)
  let pendingRollback = $state<{ plugin: Plugin; anchor?: HTMLElement } | null>(null)
  let rollingBack = $state(false)
  let pendingFile = $state<PendingFile | null>(null)
  let installingFile = $state(false)
  let togglingIds = $state<string[]>([])

  function isToggling(id: string): boolean {
    return togglingIds.includes(id)
  }

  /** applies the switch; false when the plugin keeps its state */
  async function togglePlugin(plugin: Plugin, enabled: boolean): Promise<boolean> {
    if (togglingIds.includes(plugin.id)) return false
    togglingIds = [...togglingIds, plugin.id]
    try {
      if (enabled && plugin.permissions.docker && !(await api.docker.connection()).endpoint) {
        openDockerSetup(plugin)
        return false
      }
      const rec = await api.plugins.update(plugin.id, { disabled: !enabled })
      options.onPluginSaved(rec)
      return true
    } catch (e) {
      options.onError(getErrorMessage(e))
      return false
    } finally {
      togglingIds = togglingIds.filter((id) => id !== plugin.id)
    }
  }

  /** a Docker plugin turns on once a Docker host is set up */
  function openDockerSetup(plugin: Plugin): void {
    modal.open({
      title: t('settings.docker.wizard.title'),
      content: DockerWizard,
      size: 'md',
      severity: 'medium',
      props: { onComplete: () => void togglePlugin(plugin, true) },
    })
  }

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
      options.onPluginRemoved(target.plugin.id)
    } catch (e) {
      options.onError(getErrorMessage(e))
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
      options.onPluginSaved(rec)
    } catch (e) {
      options.onError(getErrorMessage(e))
    } finally {
      rollingBack = false
    }
  }

  /** installs the store version when it requests the approved permissions */
  async function updatePlugin(plugin: Plugin, approvedDigest: string): Promise<void> {
    const update = options.findUpdate(plugin)
    const target = update ?? options.findRepoPath(plugin)
    if (!target) return
    const file = options.findStoreFile?.(plugin) ?? null
    const latest = update?.latest || file?.version || plugin.version
    try {
      const rec = await api.plugins.installFromRepo(target.repo_id, target.path, approvedDigest)
      toast.success(tx('plugins.toast.updated', { name: plugin.display_name, version: latest }))
      options.onPluginSaved(rec)
      await options.onUpdatesRefresh?.()
    } catch (e) {
      options.onError(getErrorMessage(e))
    }
  }

  let updatingAll = $state(false)

  async function updateAll(plugins: Plugin[]): Promise<void> {
    if (updatingAll) return
    const pending = plugins.filter((p) => options.findUpdate(p) !== null)
    if (pending.length === 0) return
    updatingAll = true
    const failures: string[] = []
    let succeeded = 0
    try {
      for (const plugin of pending) {
        const target = options.findUpdate(plugin) ?? options.findRepoPath(plugin)
        if (!target) {
          failures.push(`${plugin.display_name}: no repository origin`)
          continue
        }
        try {
          // Without a review only updates that keep the approved permissions install.
          const rec = await api.plugins.installFromRepo(target.repo_id, target.path, plugin.permissions.digest)
          options.onPluginSaved(rec)
          succeeded++
        } catch (e) {
          failures.push(`${plugin.display_name}: ${getErrorMessage(e)}`)
        }
      }
      await options.onUpdatesRefresh?.()
      if (failures.length === 0) {
        toast.success(tx('plugins.toast.updated_many', { count: succeeded }))
      } else {
        options.onError(failures.join('\n'))
      }
    } finally {
      updatingAll = false
    }
  }

  /** reads an uploaded file and asks the operator to approve its permissions */
  async function reviewFile(file: File, target: Plugin | null, anchor?: HTMLElement): Promise<void> {
    try {
      const source = await file.text()
      const preview = await api.plugins.previewFile(source)
      pendingFile = { source, preview, current: options.findPlugin?.(preview.id) ?? null, target, anchor }
    } catch (e) {
      options.onError(getErrorMessage(e))
    }
  }

  async function installReviewedFile(): Promise<void> {
    const pending = pendingFile
    if (!pending || installingFile) return
    installingFile = true
    try {
      const rec = await api.plugins.installFile(pending.source, pending.preview.permissions.digest)
      pendingFile = null
      if (pending.target && rec.id !== pending.target.id) {
        toast.success(tx('plugins.toast.installed_separately', { name: rec.display_name, version: rec.version }))
      } else if (pending.target) {
        toast.success(tx('plugins.toast.updated', { name: rec.display_name, version: rec.version }))
      }
      options.onPluginSaved(rec)
    } catch (e) {
      options.onError(getErrorMessage(e))
    } finally {
      installingFile = false
    }
  }

  function updateFromFile(plugin: Plugin, anchor?: HTMLElement): void {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = '.lua'
    input.onchange = () => {
      const file = input.files?.[0]
      if (file) void reviewFile(file, plugin, anchor)
    }
    input.click()
  }

  function handleAction(plugin: Plugin, id: string, anchor?: HTMLElement): void {
    switch (id) {
      case 'update_file':
        updateFromFile(plugin, anchor)
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
    togglePlugin,
    isToggling,
    handleAction,
    reviewFile,
    installReviewedFile,
    get updatingAll(): boolean {
      return updatingAll
    },
    get pendingFile(): PendingFile | null {
      return pendingFile
    },
    set pendingFile(v: PendingFile | null) {
      pendingFile = v
    },
    get installingFile(): boolean {
      return installingFile
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
