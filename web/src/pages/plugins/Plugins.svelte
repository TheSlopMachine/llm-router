<script lang="ts">
  import { api } from '$lib/api'
  import { createListResource } from '$lib/list-resource.svelte'
  import { Picker, Text, VStack, HStack } from '$ui'
  import InstalledTab from './components/InstalledTab.svelte'
  import CatalogTab from './components/CatalogTab.svelte'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '$lib/types'
  import { t } from '$lib/i18n.svelte'

  export type PluginsTab = 'installed' | 'catalog'

  let {
    tab,
    ontabchange
  } = $props<{
    tab: PluginsTab
    ontabchange: (next: PluginsTab) => void
  }>()

  interface PluginsData {
    plugins: Plugin[]
    repos: Array<{ repo: PluginRepo; files: StoreFile[]; error: string }>
    updates: PluginUpdate[]
  }

  const resource = createListResource<PluginsData>(
    async () => {
      const [plugins, search, updateList] = await Promise.all([
        api.plugins.list(),
        api.store.search().catch(() => ({ repos: [] })),
        api.store.updates().catch(() => ({ updates: [] }))
      ])
      return {
        plugins: plugins ?? [],
        repos: search.repos ?? [],
        updates: updateList.updates ?? []
      }
    },
    { plugins: [], repos: [], updates: [] }
  )

  let installedCount = $derived(resource.data.plugins.length)
  let catalogCount = $derived(resource.data.repos.reduce((n, entry) => n + entry.files.length, 0))
  let knownRepoIDs = $derived(resource.data.repos.map((entry) => entry.repo.id))

  function applySaved(plugin: Plugin): void {
    const i = resource.data.plugins.findIndex((p) => p.id === plugin.id)
    resource.data = {
      ...resource.data,
      plugins: i >= 0
        ? resource.data.plugins.map((p) => (p.id === plugin.id ? plugin : p))
        : [...resource.data.plugins, plugin],
      updates: resource.data.updates.filter((u) => u.plugin_id !== plugin.id)
    }
  }

  function applyRemoved(id: string): void {
    resource.data = {
      ...resource.data,
      plugins: resource.data.plugins.filter((p) => p.id !== id),
      updates: resource.data.updates.filter((u) => u.plugin_id !== id)
    }
  }

  async function refreshUpdates(): Promise<void> {
    try {
      const updateList = await api.store.updates().catch(() => ({ updates: [] as PluginUpdate[] }))
      resource.data = { ...resource.data, updates: updateList.updates ?? [] }
    } catch {
      // updates banner keeps stale data; banner errors surface per-action
    }
  }

  function applyRepoAdded(repo: PluginRepo): void {
    if (resource.data.repos.some((e) => e.repo.id === repo.id)) return
    resource.data = { ...resource.data, repos: [...resource.data.repos, { repo, files: [], error: '' }] }
  }

  function applyRepoRemoved(id: string): void {
    resource.data = { ...resource.data, repos: resource.data.repos.filter((e) => e.repo.id !== id) }
  }
</script>

  <VStack gap={6}>
  <VStack gap={1}>
    <Text tag="h1" size="xl" weight="bold">{t('plugins.title')}</Text>
    <Text tone="soft" size="sm">{t('plugins.manage_desc')}</Text>
  </VStack>

  <Picker
    value={tab}
    onchange={(next) => ontabchange(next as PluginsTab)}
    options={[
      { value: 'installed', label: `${t('plugins.tabs.installed')} (${installedCount})` },
      { value: 'catalog', label: `${t('plugins.tabs.catalog')} (${catalogCount})` }
    ]}
    ariaLabel={t('plugins.views')}
  />

  {#if resource.loading && resource.data.plugins.length === 0 && resource.data.repos.length === 0}
    <Text tone="soft" align="center" class="empty">{t('common.state.loading')}</Text>
  {:else if resource.error && resource.data.plugins.length === 0 && resource.data.repos.length === 0}
    <Text tone="danger" size="sm">{resource.error}</Text>
  {:else if tab === 'installed'}
    <InstalledTab
      plugins={resource.data.plugins}
      updates={resource.data.updates}
      repos={resource.data.repos}
      {knownRepoIDs}
      onPluginSaved={applySaved}
      onPluginRemoved={applyRemoved}
      onBrowseCatalog={() => ontabchange('catalog')}
    />
  {:else}
    <CatalogTab
      repos={resource.data.repos}
      plugins={resource.data.plugins}
      updates={resource.data.updates}
      onPluginSaved={applySaved}
      onPluginRemoved={applyRemoved}
      onUpdatesRefresh={refreshUpdates}
      onRepoAdded={applyRepoAdded}
      onRepoRemoved={applyRepoRemoved}
    />
  {/if}
</VStack>
