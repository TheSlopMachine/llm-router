<script lang="ts">
  import SearchField from '../ui/SearchField.svelte'
  import EmptyState from '../EmptyState.svelte'
  import PluginCard from './PluginCard.svelte'
  import PluginDetails from './PluginDetails.svelte'
  import { createPluginState } from './plugin-state.svelte'
  import type { Plugin, PluginUpdate } from '../../lib/types'

  let {
    plugins,
    updates,
    onReload,
    onBrowseCatalog
  } = $props<{
    plugins: Plugin[]
    updates: PluginUpdate[]
    onReload: () => Promise<void>
    onBrowseCatalog?: () => void
  }>()

  let query = $state('')
  let actionError = $state('')

  function findUpdate(plugin: Plugin): PluginUpdate | null {
    return updates.find((u: PluginUpdate) => u.plugin_id === plugin.id && u.update_available) ?? null
  }

  function findRepoPath(plugin: Plugin): { repo_id: string; path: string } | null {
    if (plugin.origin && !plugin.origin.manual && plugin.origin.repo_id && plugin.origin.path) {
      return { repo_id: plugin.origin.repo_id, path: plugin.origin.path }
    }
    return null
  }

  const pluginState = createPluginState({
    onReload: () => onReload(),
    onError: (message: string) => {
      actionError = message
    },
    findUpdate,
    findRepoPath
  })

  let filtered = $derived(
    query.trim()
      ? plugins.filter((p: Plugin) => {
          const q = query.trim().toLowerCase()
          return (
            p.display_name.toLowerCase().includes(q) ||
            p.id.toLowerCase().includes(q) ||
            p.description.toLowerCase().includes(q) ||
            p.type_keys.some((k: string) => k.toLowerCase().includes(q))
          )
        })
      : plugins
  )
</script>

{#if actionError}
  <div class="error-msg">{actionError}</div>
{/if}

{#if plugins.length > 0}
  <SearchField bind:value={query} placeholder="Search installed plugins..." />
{/if}

{#if plugins.length === 0}
  <EmptyState
    icon="extension"
    message="No plugins installed"
    hint="Browse the catalog to install a provider plugin."
    buttonText="Browse catalog"
    buttonIcon="download"
    onButtonClick={() => onBrowseCatalog?.()}
  />
{:else if filtered.length === 0}
  <div class="empty">No plugins match the search.</div>
{:else}
  <div class="plugin-list">
    {#each filtered as plugin (plugin.id)}
      {@const update = findUpdate(plugin)}
      <PluginCard
        title={plugin.display_name}
        meta={`v${plugin.version} · ${plugin.author}`}
        badges={[
          ...(plugin.unsafe ? [{ text: 'Unrestricted network', kind: 'badge-red' as const }] : []),
          ...(!plugin.enabled ? [{ text: 'Disabled', kind: '' as const }] : []),
          ...(update ? [{ text: `Update: v${update.current} → v${update.latest}`, kind: 'badge-green' as const }] : [])
        ]}
        subtitle={plugin.id}
        description={plugin.description}
        typeKeys={plugin.type_keys}
        mode="installed"
        actions={pluginState.buildActions(plugin)}
        onaction={(id) => pluginState.handleAction(plugin, id)}
      >
        {#if pluginState.expanded === plugin.id}
          <PluginDetails
            {plugin}
            logs={pluginState.details[plugin.id]?.logs ?? []}
            crashes={pluginState.details[plugin.id]?.crashes ?? []}
            loading={pluginState.detailsLoading[plugin.id] ?? false}
          />
        {/if}
      </PluginCard>
    {/each}
  </div>
{/if}

<style>
  .plugin-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
</style>
