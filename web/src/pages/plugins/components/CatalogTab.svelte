<script lang="ts">
  import { Button, Toolbar, ToolbarItem, FloatingView, HStack, VStack, Text, Switch, SearchField, TextEdit, List, SectionCard, Chip, Spacer, Banner, ConfirmAction } from '$ui'
  import EmptyState from '../../../FUI/composite/EmptyState.svelte'
  import PluginCard from './PluginCard.svelte'
  import RepoDetailsModal from './RepoDetailsModal.svelte'
  import { createPluginState } from './plugin-state.svelte'
  import { diffAllowHosts } from './plugin-permission-diff'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '$lib/types'
  import { api } from '$lib/api'
  import { modal } from '../../../FUI/core/modal.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import { matchesStoreFile, hasStoreOrigin, storeKey } from '$lib/plugin-search'

  interface RepoEntry {
    repo: PluginRepo
    files: StoreFile[]
    error: string
  }

  let {
    repos,
    plugins,
    updates,
    onPluginSaved,
    onPluginRemoved,
    onUpdatesRefresh,
    onRepoAdded,
    onRepoRemoved
  } = $props<{
    repos: RepoEntry[]
    plugins: Plugin[]
    updates: PluginUpdate[]
    onPluginSaved: (plugin: Plugin) => void
    onPluginRemoved: (id: string) => void
    onUpdatesRefresh: () => Promise<void>
    onRepoAdded: (repo: PluginRepo) => void
    onRepoRemoved: (id: string) => void
  }>()

  let query = $state('')
  let showUpdatesOnly = $state(false)
  let actionError = $state('')

  let showAddRepo = $state(false)
  let repoUrl = $state('')
  let adding = $state(false)

  let installingPath = $state<string | null>(null)

  function findUpdate(plugin: Plugin): PluginUpdate | null {
    return updates.find((u: PluginUpdate) => u.plugin_id === plugin.id && u.update_available) ?? null
  }

  function findRepoPath(plugin: Plugin): { repo_id: string; path: string } | null {
    if (hasStoreOrigin(plugin)) {
      if (!repos.some((entry: RepoEntry) => entry.repo.id === plugin.origin.repo_id)) {
        return null
      }
      return { repo_id: plugin.origin.repo_id, path: plugin.origin.path }
    }
    return null
  }

  const pluginState = createPluginState({
    onPluginSaved: (p) => onPluginSaved(p),
    onPluginRemoved: (id) => onPluginRemoved(id),
    onUpdatesRefresh: () => onUpdatesRefresh(),
    onError: (message: string) => {
      actionError = message
    },
    findUpdate,
    findRepoPath,
    findStoreFile: (plugin: Plugin) => {
      if (hasStoreOrigin(plugin)) {
        return fileByOrigin.get(storeKey(plugin.origin.repo_id, plugin.origin.path)) ?? null
      }
      return null
    }
  })

  let pluginByOrigin = $derived.by(() => {
    const map = new Map<string, Plugin>()
    for (const p of plugins) {
      if (hasStoreOrigin(p)) {
        map.set(storeKey(p.origin.repo_id, p.origin.path), p)
      }
    }
    return map
  })

  let availableUpdates = $derived(updates.filter((u: PluginUpdate) => u.update_available))

  let fileByOrigin = $derived.by(() => {
    const map = new Map<string, StoreFile>()
    for (const entry of repos) {
      for (const f of entry.files) {
        map.set(storeKey(f.repo_id, f.path), f)
      }
    }
    return map
  })

  function matchesQuery(f: StoreFile): boolean {
    return matchesStoreFile(f, query)
  }

  function isRepoVisible(entry: RepoEntry): boolean {
    if (entry.error) return true
    if (entry.files.length > 0) return true
    return !query.trim() && !showUpdatesOnly
  }

  let showUpdatesBanner = $derived(availableUpdates.length > 0 && !showUpdatesOnly)

  let visibleRepos = $derived.by(() => {
    return repos
      .map((entry: RepoEntry) => ({
        ...entry,
        files: entry.files.filter((f: StoreFile) => {
          if (showUpdatesOnly && !f.update_available) return false
          return matchesQuery(f)
        })
      }))
      .filter((entry: RepoEntry) => isRepoVisible(entry))
  })

  async function installEntry(repoId: string, path: string): Promise<void> {
    const key = storeKey(repoId, path)
    installingPath = key
    actionError = ''
    try {
      const rec = await api.plugins.installFromRepo(repoId, path)
      onPluginSaved(rec)
      await onUpdatesRefresh()
    } catch (e) {
      actionError = getErrorMessage(e)
    } finally {
      installingPath = null
    }
  }

  async function handleCatalogAction(plugin: Plugin, file: StoreFile, id: string, anchor?: HTMLElement): Promise<void> {
    if (id === 'update' || id === 'reinstall') {
      await installEntry(file.repo_id, file.path)
      return
    }
    await pluginState.handleAction(plugin, id, anchor)
  }

  function openRepoDetails(entry: RepoEntry): void {
    modal.open({
      title: entry.repo.title || entry.repo.id,
      content: RepoDetailsModal,
      severity: 'medium',
      size: 'small',
      props: {
        title: entry.repo.title || entry.repo.id,
        description: entry.repo.description,
        url: entry.repo.url,
        builtin: entry.repo.builtin,
        fileCount: entry.files.length
      }
    })
  }

  async function addRepo(): Promise<void> {
    adding = true
    actionError = ''
    try {
      const repo = await api.repos.addRepo(repoUrl.trim())
      showAddRepo = false
      repoUrl = ''
      onRepoAdded(repo)
    } catch (e) {
      actionError = getErrorMessage(e)
    } finally {
      adding = false
    }
  }

  let removeRepoTarget = $state<{ id: string; title: string } | null>(null)
  let removeRepoAnchor = $state<HTMLElement>()
  let removingRepo = $state(false)

  function openRemoveRepo(entry: RepoEntry, anchorEl?: HTMLElement): void {
    if (removeRepoTarget?.id === entry.repo.id) {
      removeRepoTarget = null
    } else {
      removeRepoTarget = { id: entry.repo.id, title: entry.repo.title || entry.repo.id }
      removeRepoAnchor = anchorEl
    }
  }

  async function confirmRemoveRepo(): Promise<void> {
    const target = removeRepoTarget
    if (!target || removingRepo) return
    removingRepo = true
    try {
      await api.repos.remove(target.id)
      removeRepoTarget = null
      onRepoRemoved(target.id)
    } catch (e) {
      actionError = getErrorMessage(e)
    } finally {
      removingRepo = false
    }
  }
</script>

<VStack gap={4}>
  {#if actionError}
    <Banner variant="error" text={actionError} />
  {/if}

  <Toolbar overflow="menu" gap={3} moreLabel={t('providers.detail.more_actions')}>
    <ToolbarItem pinned fill="md">
      <SearchField bind:value={query} placeholder={t('plugins.search.catalog')} />
    </ToolbarItem>
    <ToolbarItem priority={2} active={showUpdatesOnly}>
      <Switch bind:checked={showUpdatesOnly} label={t('plugins.updates_only')} ariaLabel={t('plugins.updates_only')} />
    </ToolbarItem>
    <ToolbarItem priority={1}>
      <Button
        style="prominent"
        icon={{ name: 'add' }}
        text={t('plugins.repo.add')}
        onclick={() => { showAddRepo = !showAddRepo }}
      />
    </ToolbarItem>
  </Toolbar>

  {#if showAddRepo}
    <SectionCard title={t('plugins.repo.add')} description={t('plugins.repo.url_hint')}>
      <VStack gap={4}>
        <VStack gap={1}>
          <Text size="sm" weight="medium" tag="label" for="repo-url">{t('plugins.repo.url_label')}</Text>
          <TextEdit id="repo-url" bind:value={repoUrl} hint="https://github.com/..." />
        </VStack>
        <HStack gap={2} justify="end">
          <Button onclick={() => { showAddRepo = false }}>{t('common.actions.cancel')}</Button>
          <Button style="prominent" onclick={addRepo} disabled={adding || !repoUrl.trim()}>{t('common.actions.add')}</Button>
        </HStack>
      </VStack>
    </SectionCard>
  {/if}

  {#if showUpdatesBanner}
    <SectionCard title={t('plugins.updates_available')}>
      <List>
        {#each availableUpdates as u}
          <div style="padding: var(--fui-space-3) var(--fui-space-4);">
            <HStack align="center" gap={3}>
              <Text size="sm">{u.plugin_id}: {u.current} → {u.latest}</Text>
              <Spacer />
              <Button style="none" size="small" onclick={() => void installEntry(u.repo_id, u.path)}>{t('common.actions.update')}</Button>
            </HStack>
          </div>
        {/each}
      </List>
    </SectionCard>
  {/if}

  {#if repos.length === 0}
    {#snippet addRepoAction()}
      <Button style="prominent" icon={{ name: 'add' }} onclick={() => { showAddRepo = true }}>{t('plugins.repo.add')}</Button>
    {/snippet}
    <EmptyState
      icon="download"
      title={t('plugins.repo.empty')}
      caption={t('plugins.repo.add_short')}
      action={addRepoAction}
    />
  {:else if visibleRepos.length === 0}
    <Text tone="soft" align="center">{t('plugins.catalog.no_filter_match')}</Text>
  {:else}
    {#each visibleRepos as entry (entry.repo.id)}
      <VStack gap={3}>
        <HStack align="center" gap={3}>
          <VStack gap={0} grow>
            <Text variant="section-title">{entry.repo.title || entry.repo.id}</Text>
            {#if entry.repo.description}<Text size="xs" tone="soft">{entry.repo.description}</Text>{/if}
          </VStack>
          <HStack gap={2}>
            {#if entry.repo.builtin}<Chip text={t('plugins.repo.builtin')} color="chip-neutral" size="small" />{/if}
            <Button
              style="text"
              icon={{ name: 'info' }}
              size="small"
              onclick={() => openRepoDetails(entry)}
              ariaLabel={t('plugins.repo.details')}
            />
            {#if !entry.repo.builtin}
              <Button
                style="text"
                tint="var(--fui-color-danger)"
                icon={{ name: 'delete' }}
                size="small"
                onclick={(e) => openRemoveRepo(entry, e.currentTarget as HTMLElement)}
                ariaLabel={t('plugins.repo.remove')}
              />
            {/if}
          </HStack>
        </HStack>

        {#if entry.error}
          <Banner variant="error" text={entry.error} />
        {:else if entry.files.length === 0}
          <EmptyState title={t('plugins.repo.no_plugins')} icon="extension" />
        {:else}
          <List>
            {#each entry.files as f (storeKey(f.repo_id, f.path))}
              {@const plugin = pluginByOrigin.get(storeKey(f.repo_id, f.path)) ?? null}
              {@const key = storeKey(f.repo_id, f.path)}
              {#if plugin}
                {@const update = findUpdate(plugin)}
                {@const newHosts = update?.new_allow_hosts ?? f.allow_hosts ?? []}
                {@const newUnsafe = update?.new_unsafe ?? f.unsafe}
                {@const diff = diffAllowHosts(plugin.allow_hosts ?? [], plugin.unsafe, newHosts, newUnsafe)}
                <PluginCard
                  title={f.display_name || f.path}
                  version={`v${plugin.version}`}
                  description={f.description}
                  mode="installed"
                  unsafe={plugin.unsafe}
                  hasUpdate={!!update}
                  allowHosts={plugin.allow_hosts ?? []}
                  newHosts={newHosts}
                  added={diff.added}
                  removed={diff.removed}
                  escalatesToUnsafe={diff.escalatesToUnsafe}
                  latestVersion={update?.latest ?? f.version}
                  onUpdate={() => void installEntry(f.repo_id, f.path)}
                  onaction={(id, anchor) => handleCatalogAction(plugin, f, id, anchor)}
                />
              {:else}
                <PluginCard
                  title={f.display_name || f.path}
                  version={f.version ? `v${f.version}` : ''}
                  description={f.description}
                  mode="uninstalled"
                  unsafe={f.unsafe}
                  installing={installingPath === key}
                  allowHosts={f.allow_hosts ?? []}
                  onInstall={() => void installEntry(f.repo_id, f.path)}
                />
              {/if}
              {#if f.error}<Text tone="danger" variant="caption">{f.error}</Text>{/if}
            {/each}
          </List>
        {/if}
      </VStack>
    {/each}
  {/if}

  <FloatingView
    open={Boolean(removeRepoTarget)}
    anchor={removeRepoAnchor}
    width="sm"
    onclose={() => { removeRepoTarget = null }}
    label={t('plugins.repo.remove')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('plugins.repo.remove')}
        body={t('plugins.repo.remove_confirm')}
        confirmLabel={t('common.actions.remove')}
        busy={removingRepo}
        busyLabel={t('common.actions.removing')}
        onCancel={close}
        onConfirm={confirmRemoveRepo} />
    {/snippet}
  </FloatingView>

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


