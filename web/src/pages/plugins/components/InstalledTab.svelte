<script lang="ts">
  import { SearchField, Toolbar, ToolbarItem, Button, FloatingView, List, VStack, HStack, Text, Banner, ConfirmAction, FilePicker, ContentUnavailable } from '@fuckyouryan/fui'
  import PluginCard from './PluginCard.svelte'
  import InstallConfirm from './InstallConfirm.svelte'
  import { createPluginState } from './plugin-state.svelte'
  import { diffPermissions } from '$lib/plugin-permissions'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '$lib/types'
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
  }: {
    plugins: Plugin[]
    updates: PluginUpdate[]
    repos: Array<{ repo: PluginRepo; files: StoreFile[]; error: string }>
    knownRepoIDs: string[]
    onPluginSaved: (plugin: Plugin) => void
    onPluginRemoved: (id: string) => void
    onBrowseCatalog?: () => void
  } = $props()

  let query = $state('')
  let actionError = $state('')
  let uploadAnchor = $state<HTMLElement>()

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
    findStoreFile,
    findPlugin: (id) => plugins.find((plugin) => plugin.id === id) ?? null
  })

  let updateCount = $derived(updates.filter((u: PluginUpdate) => u.update_available).length)

  let filtered = $derived(
    query.trim()
      ? plugins.filter((p: Plugin) => matchesPlugin(p, query))
      : plugins
  )
</script>

<VStack gap={4}>
  {#if actionError}
    <Banner tint="danger" text={actionError} />
  {/if}

  <div class="installed-toolbar">
    <Toolbar overflow="wrap" gap={3}>
      <ToolbarItem pinned fill="md">
        <SearchField bind:value={query} placeholder={t('plugins.search.installed')} />
      </ToolbarItem>
      <div class="toolbar-spacer" aria-hidden="true"></div>
      <div bind:this={uploadAnchor} class="upload-anchor">
        <FilePicker
          accept=".lua"
          label={t('plugins.upload_file')}
          buttonText={t('upload.file')}
          icon="upload_file"
          disabled={pluginState.installingFile}
          onpick={(files) => { actionError = ''; void pluginState.reviewFile(files[0], null, uploadAnchor) }}
        />
      </div>
      {#if updateCount > 0}
        <ToolbarItem priority={1}>
          <Button
            buttonStyle="prominent"
            icon={{ name: 'upgrade' }}
            text={pluginState.updatingAll ? t('plugins.updating') : `${t('plugins.update_all')} (${updateCount})`}
            disabled={pluginState.updatingAll}
            onclick={() => void pluginState.updateAll(plugins)}
          />
        </ToolbarItem>
      {/if}
    </Toolbar>
  </div>

  {#if plugins.length === 0}
    {#snippet browseCatalogAction()}
      <Button buttonStyle="prominent" icon={{ name: 'download' }} onclick={() => onBrowseCatalog?.()}>{t('plugins.installed.browse_catalog')}</Button>
    {/snippet}
    <ContentUnavailable
      icon="extension"
      title={t('plugins.installed.empty')}
      caption={t('plugins.installed.browse')}
      action={browseCatalogAction}
    />
  {:else if filtered.length === 0}
    <ContentUnavailable title={t('plugins.catalog.no_match')} icon="search" />
  {:else}
    <List>
      {#each filtered as plugin (plugin.id)}
        {@const update = findUpdate(plugin)}
        {@const review = update?.new_permissions ?? plugin.permissions}
        <PluginCard
          title={plugin.display_name}
          version={`v${plugin.version}`}
          description={plugin.description}
          iconUrl={plugin.icon_url}
          subsystems={plugin.subsystems}
          mode="installed"
          permissions={plugin.permissions}
          {review}
          diff={diffPermissions(plugin.permissions, review)}
          disabled={plugin.disabled}
          toggling={pluginState.isToggling(plugin.id)}
          isManual={plugin.origin.manual}
          hasUpdate={!!update}
          onUpdate={() => void pluginState.updatePlugin(plugin, review.digest)}
          onToggle={(enabled) => pluginState.togglePlugin(plugin, enabled)}
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
        oncancel={close}
        onconfirm={() => void pluginState.doDelete()} />
    {/snippet}
  </FloatingView>

  <FloatingView
    open={Boolean(pluginState.pendingFile)}
    anchor={pluginState.pendingFile?.anchor}
    width="md"
    onclose={() => { pluginState.pendingFile = null }}
    label={t('plugins.install')}
  >
    {#snippet children({ close })}
      {#if pluginState.pendingFile}
        {@const pending = pluginState.pendingFile}
        <InstallConfirm
          permissions={pending.preview.permissions}
          diff={pending.current ? diffPermissions(pending.current.permissions, pending.preview.permissions) : null}
          confirmLabel={pending.current ? t('common.actions.update') : t('plugins.install')}
          busy={pluginState.installingFile}
          oncancel={close}
          onconfirm={() => void pluginState.installReviewedFile()}
        />
      {/if}
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
          <Text weight="medium" size="md">{t('plugins.rollback_title')}</Text>
          <Text size="sm" foregroundStyle="secondary">
            {t('plugins.rollback')} "{pluginState.pendingRollback?.plugin.display_name}" {t('plugins.rollback_confirm')}
          </Text>
        </VStack>
        <HStack halign="end" gap={2}>
          <Button size="sm" buttonStyle="text" onclick={close} disabled={pluginState.rollingBack}>{t('common.actions.cancel')}</Button>
          <Button
            size="sm"
            buttonStyle="prominent"
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

<style>
  /* FUI's toolbar row is content-sized; stretch it so toolbar-spacer can
     grow and push the right-docked Upload file and Update all buttons to
     the far edge. */
  .installed-toolbar :global(.tbar-row) {
    flex: 1 1 auto;
  }
  /* The file review popover anchors to the upload button. */
  .upload-anchor {
    display: inline-flex;
  }
  .toolbar-spacer {
    flex: 1 1 auto;
    margin-inline-start: auto;
    min-width: 0;
  }
</style>

