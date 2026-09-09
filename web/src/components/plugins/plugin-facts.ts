import type { Plugin, StoreFile } from '../../lib/types'

// PluginFacts is the shared view-model for the details and install modals.
export interface PluginFacts {
  title: string
  versionLine: string
  description: string
  idLine: string
  typeKeys: string[]
  allowHosts: string[]
  unsafe: boolean
}

export function factsFromPlugin(plugin: Plugin): PluginFacts {
  return {
    title: plugin.display_name,
    versionLine: `v${plugin.version} · ${plugin.author}`,
    description: plugin.description,
    idLine: plugin.id,
    typeKeys: [...plugin.type_keys],
    allowHosts: [...plugin.allow_hosts],
    unsafe: plugin.unsafe
  }
}

export function factsFromFile(file: StoreFile): PluginFacts {
  return {
    title: file.display_name || file.path,
    versionLine: file.version ? `v${file.version}` : '',
    description: file.description,
    idLine: file.path,
    typeKeys: [],
    allowHosts: [...(file.allow_hosts ?? [])],
    unsafe: file.unsafe
  }
}
