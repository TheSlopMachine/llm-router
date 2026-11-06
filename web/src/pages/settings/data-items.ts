// File formats of the data management cards. Every per-item file except a
// plugin's is in a format its subsystem's import takes back:
//   providers       the per-provider export endpoint's bundle object
//   virtual_models  one model object from the subsystem export
//   tokens          the subsystem export filtered to one token
//   plugins         the plugin source as <id>.lua (installed through Plugins)
// The plugins subsystem file is a zip archive; the other subsystems are JSON.
import { modal } from '@fuckyouryan/fui'
import { api } from '$lib/api'
import { t } from '$lib/i18n.svelte'
import { downloadBlob, downloadJson, pickFile, pickJsonFile } from '$lib/download'

export interface DataItem {
  id: string
  label: string
}

export interface ItemExport {
  list(): Promise<DataItem[]>
  /** downloads the file holding this item only */
  exportItem(item: DataItem): Promise<void>
}

/** The subsystems in clear order: providers first, so their managed virtual models go with them. */
export const subsystemKeys = ['providers', 'virtual_models', 'plugins', 'tokens', 'proxy'] as const

interface ExportRecord {
  id: string
  name?: string
}

function records(value: unknown): ExportRecord[] {
  return Array.isArray(value) ? (value as ExportRecord[]) : []
}

function itemFileName(subsystem: string, id: string): string {
  return `llm_router_${subsystem}_${id}_export.json`
}

function filteredExport(subsystem: 'virtual_models' | 'tokens', single: boolean): ItemExport {
  return {
    async list() {
      const all = records(await api.data.exportSubsystem(subsystem))
      return all.map((record) => ({ id: record.id, label: record.name || record.id }))
    },
    async exportItem(item) {
      const matching = records(await api.data.exportSubsystem(subsystem)).filter((record) => record.id === item.id)
      downloadJson(itemFileName(subsystem, item.id), single ? matching[0] : matching)
    },
  }
}

const pluginExport: ItemExport = {
  async list() {
    const plugins = await api.plugins.list()
    return plugins.map((plugin) => ({ id: plugin.id, label: plugin.display_name || plugin.id }))
  },
  async exportItem(item) {
    // plugin IDs are author/plugin; a file name cannot hold the slash
    downloadBlob(`${item.id.replaceAll('/', '_')}.lua`, await api.plugins.source(item.id))
  },
}

const providerExport: ItemExport = {
  async list() {
    const providers = await api.providers.list()
    return providers.map((provider) => ({ id: provider.id, label: provider.name || provider.id }))
  },
  async exportItem(item) {
    downloadJson(itemFileName('providers', item.id), await api.data.exportProvider(item.id))
  },
}

export const itemExports: Record<string, ItemExport> = {
  providers: providerExport,
  virtual_models: filteredExport('virtual_models', true),
  tokens: filteredExport('tokens', false),
  plugins: pluginExport,
}

/** Downloads the subsystem file. */
export async function exportSubsystem(subsystem: string): Promise<void> {
  if (subsystem === 'plugins') {
    downloadBlob('llm_router_plugins_export.zip', await api.data.exportPluginsArchive())
    return
  }
  downloadJson(`llm_router_${subsystem}_export.json`, await api.data.exportSubsystem(subsystem))
}

/** Picks a subsystem file (for providers and virtual models also a per-item file) and imports it; false when nothing was picked. */
export async function importSubsystem(subsystem: string): Promise<boolean> {
  if (subsystem === 'plugins') {
    const archive = await pickFile('.zip,application/zip')
    if (archive === null) return false
    await api.data.importPluginsArchive(archive)
    return true
  }
  const json = await pickJsonFile()
  if (json === null) return false
  await api.data.importSubsystem(subsystem, json)
  return true
}

/** Clears every subsystem after one destructive confirm; false when declined. */
export async function clearAllData(): Promise<boolean> {
  const confirmed = await modal.confirm({
    title: t('data.clear_all'),
    message: t('data.clear_all_confirm'),
    confirmText: t('data.clear'),
    confirmRole: 'destructive',
  })
  if (!confirmed) return false
  for (const subsystem of subsystemKeys) await api.data.clearSubsystem(subsystem)
  return true
}
