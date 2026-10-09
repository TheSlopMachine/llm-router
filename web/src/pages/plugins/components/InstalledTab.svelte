<script lang="ts">
  import { SearchField, Toolbar, ToolbarItem, Button, FloatingView, List, VStack, HStack, Text, Banner, ConfirmAction, FilePicker } from '$ui'
  import EmptyState from '../../../FUI/composite/EmptyState.svelte'
  import PluginCard from './PluginCard.svelte'
  import { createPluginState } from './plugin-state.svelte'
  import { diffAllowHosts } from './plugin-permission-diff'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '$lib/types'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import { matchesPlugin, hasStoreOrigin, storeKey } from '$lib/plugin-search'

  let {
    plugins,
    updates,
    repos,
    knownRepoIDs,
    onPluginSaved,
    onPluginRemoved,
    onBrowseCatalog
  } = $props<{
    plugins: Plugin[]
    updates: PluginUpdate[]
    repos: Array<{ repo: PluginRepo; files: StoreFile[]; error: string }>
    knownRepoIDs: string[]
    onPluginSaved: (plugin: Plugin) => void
    onPluginRemoved: (id: string) => void
    onBrowseCatalog?: () => void
  }>()

  let query = $state('')
  let actionError = $state('')
  let uploading = $state(false)
  let uploadLabel = $derived(uploading ? t('plugins.uploading') : t('upload.file'))

  function findUpdate(plugin: Plugin): PluginUpdate | null {
    return updates.find((u: PluginUpdate) => u.plugin_id === plugin.id && u.update_available) ?? null
  }

  function findRepoPath(plugin: Plugin): { repo_id: string; path: string } | null {
    if (hasStoreOrigin(plugin)) {
      if (!knownRepoIDs.includes(plugin.origin.repo_id)) {
        return null
      }
      return { repo_id: plugin.origin.repo_id, path: plugin.origin.path }
    }
    return null
  }

  let storeFileByOrigin = $derived.by(() => {
    const map = new Map<string, StoreFile>()
    for (const entry of repos) {
      for (const f of entry.files) {
        map.set(storeKey(f.repo_id, f.path), f)
      }
    }
    return map
  })

  function findStoreFile(plugin: Plugin): StoreFile | null {
    if (hasStoreOrigin(plugin)) {
      return storeFileByOrigin.get(storeKey(plugin.origin.repo_id, plugin.origin.path)) ?? null
    }
    return null
  }

  const pluginState = createPluginState({
    onPluginSaved: (p) => onPluginSaved(p),
    onPluginRemoved: (id) => onPluginRemoved(id),
    onError: (message: string) => {
      actionError = message
    },
    findUpdate,
    findRepoPath,
    findStoreFile
  })

  let updateCount = $derived(updates.filter((u: PluginUpdate) => u.update_available).length)

  let filtered = $derived(
    query.trim()
      ? plugins.filter((p: Plugin) => matchesPlugin(p, query))
      : plugins
  )

  async function uploadPickedFile(file: File): Promise<void> {
    uploading = true
    actionError = ''
    try {
      const text = await file.text()
      const rec = await api.plugins.installFile(text)
      onPluginSaved(rec)
    } catch (err) {
      actionError = getErrorMessage(err)
    } finally {
      uploading = false
    }
  }
</script>

<VStack gap={4}>
  {#if actionError}
    <Banner variant="error" text={actionError} />
  {/if}

  <Toolbar overflow="wrap" gap={3}>
    <ToolbarItem pinned fill="md">
      <SearchField bind:value={query} placeholder={t('plugins.search.installed')} />
    </ToolbarItem>
    <FilePicker
      accept=".lua"
      label={t('plugins.upload_file')}
      buttonText={uploadLabel}
      icon="upload_file"
      disabled={uploading}
      onPick={(file) => void uploadPickedFile(file)} />
    {#if updateCount > 0}
      <ToolbarItem priority={1}>
        <Button
          style="prominent"
          icon={{ name: 'upgrade' }}
          text={pluginState.updatingAll ? t('plugins.updating') : `${t('plugins.update_all')} (${updateCount})`}
          disabled={pluginState.updatingAll}
          onclick={() => void pluginState.updateAll(plugins)}
        />
      </ToolbarItem>
    {/if}
  </Toolbar>

  {#if plugins.length === 0}
    {#snippet browseCatalogAction()}
      <Button style="prominent" icon={{ name: 'download' }} onclick={() => onBrowseCatalog?.()}>{t('plugins.installed.browse_catalog')}</Button>
    {/snippet}
    <EmptyState
      icon="extension"
      title={t('plugins.installed.empty')}
      caption={t('plugins.installed.browse')}
      action={browseCatalogAction}
    />
  {:else if filtered.length === 0}
    <EmptyState title={t('plugins.catalog.no_match')} icon="search" />
  {:else}
    <List>
      {#each filtered as plugin (plugin.id)}
        {@const update = findUpdate(plugin)}
        {@const file = findStoreFile(plugin)}
        {@const newHosts = update?.new_allow_hosts ?? file?.allow_hosts ?? []}
        {@const newUnsafe = update?.new_unsafe ?? file?.unsafe ?? plugin.unsafe}
        {@const diff = diffAllowHosts(plugin.allow_hosts ?? [], plugin.unsafe, newHosts, newUnsafe)}
        <PluginCard
          title={plugin.display_name}
          version={`v${plugin.version}`}
          description={plugin.description}
          mode="installed"
          unsafe={plugin.unsafe}
          isManual={plugin.origin?.manual ?? false}
          hasUpdate={!!update}
          allowHosts={plugin.allow_hosts ?? []}
          newHosts={newHosts}
          added={diff.added}
          removed={diff.removed}
          escalatesToUnsafe={diff.escalatesToUnsafe}
          latestVersion={update?.latest ?? file?.version ?? ''}
          onUpdate={() => void pluginState.updatePlugin(plugin)}
          onaction={(id, anchor) => pluginState.handleAction(plugin, id, anchor)}
        />
      {/each}
    </List>
  {/if}

  <FloatingView
    open={Boolean(pluginState.pendingDelete)}
    anchor={pluginState.pendingDelete?.anchor}
    width="sm"
    onclose={() => { pluginState.pendingDelete = null }}
    label={t('plugins.delete')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('plugins.delete')}
        body={`${t('common.actions.delete')} "${pluginState.pendingDelete?.plugin.display_name}"? ${t('plugins.delete_warning')}`}
        busy={pluginState.deleting}
        busyLabel={t('common.actions.deleting')}
        onCancel={close}
        onConfirm={() => void pluginState.doDelete()} />
    {/snippet}
  </FloatingView>

  <FloatingView
    open={Boolean(pluginState.pendingRollback)}
    anchor={pluginState.pendingRollback?.anchor}
    width="sm"
    onclose={() => { pluginState.pendingRollback = null }}
    label={t('plugins.rollback_title')}
  >
    {#snippet children({ close })}
      <VStack gap={3}>
        <VStack gap={1}>
          <Text weight="medium" size="base">{t('plugins.rollback_title')}</Text>
          <Text size="sm" tone="soft">
            {t('plugins.rollback')} "{pluginState.pendingRollback?.plugin.display_name}" {t('plugins.rollback_confirm')}
          </Text>
        </VStack>
        <HStack justify="end" gap={2}>
          <Button size="small" style="text" onclick={close} disabled={pluginState.rollingBack}>{t('common.actions.cancel')}</Button>
          <Button
            size="small"
            style="prominent"
            disabled={pluginState.rollingBack}
            onclick={() => void pluginState.doRollback()}
          >
            {pluginState.rollingBack ? t('plugins.rollbacking') : t('plugins.rollback_title')}
          </Button>
        </HStack>
      </VStack>
    {/snippet}
  </FloatingView>
</VStack>


