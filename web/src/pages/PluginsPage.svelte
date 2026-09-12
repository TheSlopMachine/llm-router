<script lang="ts">
  import { api } from '../lib/api'
  import { createListResource } from '../lib/list-resource.svelte'
  import SegmentedControl from '../components/ui/SegmentedControl.svelte'
  import InstalledTab from '../components/plugins/InstalledTab.svelte'
  import CatalogTab from '../components/plugins/CatalogTab.svelte'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '../lib/types'
  import { t } from '../lib/i18n.svelte'

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

<div class="page-header">
  <div>
    <h1>{t('Plugins')}</h1>
    <p>{t('Manage installed plugins and install new ones from the catalog.')}</p>
  </div>
</div>

<div class="tabs-row">
  <SegmentedControl
    value={tab}
    onchange={(next) => ontabchange(next as PluginsTab)}
    options={[
      { value: 'installed', label: `${t('Installed')} (${installedCount})` },
      { value: 'catalog', label: `${t('Catalog')} (${catalogCount})` }
    ]}
    ariaLabel={t('Plugin views')}
  />
</div>

{#if resource.loading}
  <div class="empty">{t('Loading…')}</div>
{:else if resource.error}
  <div class="error-msg">{resource.error}</div>
{:else if tab === 'installed'}
  <InstalledTab
    plugins={resource.data.plugins}
    updates={resource.data.updates}
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

<style>
  .tabs-row {
    margin-bottom: 20px;
  }
</style>
