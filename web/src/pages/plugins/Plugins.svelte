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

  {#if resource.loading}
    <Text tone="soft" align="center" class="empty">{t('common.state.loading')}</Text>
  {:else if resource.error}
    <Text tone="danger" size="sm">{resource.error}</Text>
  {:else if tab === 'installed'}
    <InstalledTab
      plugins={resource.data.plugins}
      updates={resource.data.updates}
      repos={resource.data.repos}
      {knownRepoIDs}
      onReload={resource.reload}
      onBrowseCatalog={() => ontabchange('catalog')}
    />
  {:else}
    <CatalogTab
      repos={resource.data.repos}
      plugins={resource.data.plugins}
      updates={resource.data.updates}
      onReload={resource.reload}
    />
  {/if}
</VStack>
