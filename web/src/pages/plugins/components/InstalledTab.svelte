<script lang="ts">
  import { SearchField, Button, FloatingView, List, VStack, HStack, Text } from '$ui'
  import EmptyState from '../../../components/EmptyState.svelte'
  import PluginCard from './PluginCard.svelte'
  import { createPluginState } from './plugin-state.svelte'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '$lib/types'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'

  let {
    plugins,
    updates,
    repos,
    knownRepoIDs,
    onReload,
    onBrowseCatalog
  } = $props<{
    plugins: Plugin[]
    updates: PluginUpdate[]
    repos: Array<{ repo: PluginRepo; files: StoreFile[]; error: string }>
    knownRepoIDs: string[]
    onReload: () => Promise<void>
    onBrowseCatalog?: () => void
  }>()

  let query = $state('')
  let actionError = $state('')
  let uploading = $state(false)
  let fileInput = $state<HTMLInputElement>()

  function findUpdate(plugin: Plugin): PluginUpdate | null {
    return updates.find((u: PluginUpdate) => u.plugin_id === plugin.id && u.update_available) ?? null
  }

  function findRepoPath(plugin: Plugin): { repo_id: string; path: string } | null {
    if (plugin.origin && !plugin.origin.manual && plugin.origin.repo_id && plugin.origin.path) {
      if (!knownRepoIDs.includes(plugin.origin.repo_id)) {
        return null
      }
      return { repo_id: plugin.origin.repo_id, path: plugin.origin.path }
    }
    return null
  }

  function getPluginOriginText(plugin: Plugin): string {
    if (plugin.origin?.manual) {
      return t('Installed manually')
    }
    if (plugin.origin?.repo_id) {
      const match = repos.find((r: { repo: PluginRepo }) => r.repo.id === plugin.origin.repo_id)
      return match?.repo.title || plugin.origin.repo_id
    }
    return t('Unknown repository')
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

  async function uploadFile(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    uploading = true
    actionError = ''
    try {
      const text = await file.text()
      await api.plugins.installFile(text)
      await onReload()
    } catch (err) {
      actionError = getErrorMessage(err)
    } finally {
      uploading = false
      input.value = ''
    }
  }
</script>

<VStack gap={4}>
  {#if actionError}
    <Text tone="danger" size="sm">{actionError}</Text>
  {/if}

  <HStack align="center" gap={3}>
    <div style="flex: 1;">
      <SearchField bind:value={query} placeholder={t('Search installed plugins...')} />
    </div>
    <input
      bind:this={fileInput}
      type="file"
      accept=".lua"
      onchange={uploadFile}
      disabled={uploading}
      style="display: none;"
    />
    <Button
      style="none"
      icon={{ name: 'upload_file' }}
      text={uploading ? t('Uploading…') : t('Upload file')}
      disabled={uploading}
      onclick={() => fileInput?.click()}
    />
  </HStack>

  {#if plugins.length === 0}
    {#snippet browseCatalogAction()}
      <Button style="prominent" icon={{ name: 'download' }} onclick={() => onBrowseCatalog?.()}>{t('Browse catalog')}</Button>
    {/snippet}
    <EmptyState
      icon="extension"
      title={t('No plugins installed')}
      caption={t('Browse the catalog to install a provider plugin.')}
      action={browseCatalogAction}
    />
  {:else if filtered.length === 0}
    <Text tone="soft" align="center">{t('No plugins match the search.')}</Text>
  {:else}
    <List>
      {#each filtered as plugin (plugin.id)}
        {@const update = findUpdate(plugin)}
        <PluginCard
          title={plugin.display_name}
          version={`v${plugin.version}`}
          origin={getPluginOriginText(plugin)}
          description={plugin.description}
          mode="installed"
          unsafe={plugin.unsafe}
          isManual={plugin.origin?.manual ?? false}
          hasUpdate={!!update}
          onDetails={() => pluginState.openDetails(plugin)}
          onaction={(id, anchor) => pluginState.handleAction(plugin, id, anchor)}
        />
      {/each}
    </List>
  {/if}

  <FloatingView
    open={Boolean(pluginState.pendingDelete)}
    anchor={pluginState.pendingDelete?.anchor}
    onclose={() => { pluginState.pendingDelete = null }}
    label={t('Delete plugin')}
  >
    {#snippet children({ close })}
      <VStack gap={3} style="max-width: 280px;">
        <VStack gap={1}>
          <Text weight="medium" size="base">{t('Delete plugin')}</Text>
          <Text size="sm" tone="soft">
            {t('Delete')} "{pluginState.pendingDelete?.plugin.display_name}"? {t('Providers using its types will stop working.')}
          </Text>
        </VStack>
        <HStack justify="end" gap={2}>
          <Button size="small" style="text" onclick={close} disabled={pluginState.deleting}>{t('Cancel')}</Button>
          <Button
            size="small"
            style="prominent"
            tint="#dc2626"
            disabled={pluginState.deleting}
            onclick={() => void pluginState.doDelete()}
          >
            {pluginState.deleting ? t('Deleting…') : t('Delete')}
          </Button>
        </HStack>
      </VStack>
    {/snippet}
  </FloatingView>

  <FloatingView
    open={Boolean(pluginState.pendingRollback)}
    anchor={pluginState.pendingRollback?.anchor}
    onclose={() => { pluginState.pendingRollback = null }}
    label={t('Roll back plugin')}
  >
    {#snippet children({ close })}
      <VStack gap={3} style="max-width: 280px;">
        <VStack gap={1}>
          <Text weight="medium" size="base">{t('Roll back plugin')}</Text>
          <Text size="sm" tone="soft">
            {t('Roll')} "{pluginState.pendingRollback?.plugin.display_name}" {t('back to the previous version?')}
          </Text>
        </VStack>
        <HStack justify="end" gap={2}>
          <Button size="small" style="text" onclick={close} disabled={pluginState.rollingBack}>{t('Cancel')}</Button>
          <Button
            size="small"
            style="prominent"
            disabled={pluginState.rollingBack}
            onclick={() => void pluginState.doRollback()}
          >
            {pluginState.rollingBack ? t('Rolling back…') : t('Roll back')}
          </Button>
        </HStack>
      </VStack>
    {/snippet}
  </FloatingView>
</VStack>


