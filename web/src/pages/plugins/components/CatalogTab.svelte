<script lang="ts">
  import { Button, Toolbar, ToolbarItem, ToolbarSpacer, DeleteButton, Section, Text, Toggle, SearchField, List, ListRow, Card, Chip, Banner, ContentUnavailable, modal, confirmAt } from '@fuckyouryan/fui'
  import PluginCard from './PluginCard.svelte'
  import RepoDetailsModal from './RepoDetailsModal.svelte'
  import AddRepositoryModal from './AddRepositoryModal.svelte'
  import { openFormModal } from '$lib/modal-helpers'
  import { createPluginState } from './plugin-state.svelte'
  import { diffPermissions } from '$lib/plugin-permissions'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '$lib/types'
  import { api } from '$lib/api'
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
    onRepoRemoved,
    onRefreshRepos
  }: {
    repos: RepoEntry[]
    plugins: Plugin[]
    updates: PluginUpdate[]
    onPluginSaved: (plugin: Plugin) => void
    onPluginRemoved: (id: string) => void
    onUpdatesRefresh: () => Promise<void>
    onRepoAdded: (repo: PluginRepo) => void
    onRepoRemoved: (id: string) => void
    onRefreshRepos: () => Promise<void>
  } = $props()

  let query = $state('')
  let showUpdatesOnly = $state(false)
  let actionError = $state('')

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

  /** installs the file when it requests the approved permissions */
  async function installEntry(repoId: string, path: string, approvedDigest: string): Promise<void> {
    const key = storeKey(repoId, path)
    installingPath = key
    actionError = ''
    try {
      const rec = await api.plugins.installFromRepo(repoId, path, approvedDigest)
      onPluginSaved(rec)
      await onUpdatesRefresh()
    } catch (e) {
      actionError = getErrorMessage(e)
    } finally {
      installingPath = null
    }
  }

  /** the permissions an update keeps: without a review only those install */
  function installedDigest(update: PluginUpdate): string | null {
    return plugins.find((plugin: Plugin) => plugin.id === update.plugin_id)?.permissions.digest ?? null
  }

  function openRepoDetails(entry: RepoEntry): void {
    modal.open({
      title: entry.repo.title || entry.repo.id,
      content: RepoDetailsModal,
      severity: 'medium',
      size: 'sm',
      props: {
        title: entry.repo.title || entry.repo.id,
        description: entry.repo.description,
        url: entry.repo.url,
        builtin: entry.repo.builtin,
        fileCount: entry.files.length
      }
    })
  }

  function openAddRepo(): void {
    actionError = ''
    openFormModal(AddRepositoryModal, { title: t('plugins.repo.add'), size: 'sm', props: { onAdded: onRepoAdded } })
  }

  let refreshing = $state(false)

  async function refreshRepos(): Promise<void> {
    if (refreshing) return
    refreshing = true
    actionError = ''
    try {
      await onRefreshRepos()
    } catch (e) {
      actionError = getErrorMessage(e)
    } finally {
      refreshing = false
    }
  }

  async function removeRepo(entry: RepoEntry, anchor: HTMLElement): Promise<void> {
    actionError = ''
    try {
      const removed = await confirmAt(anchor, {
        title: t('plugins.repo.remove'),
        body: t('plugins.repo.remove_confirm'),
        confirmLabel: t('common.actions.remove'),
        busyLabel: t('common.actions.removing'),
        run: () => api.repos.remove(entry.repo.id),
      })
      if (removed) onRepoRemoved(entry.repo.id)
    } catch (e) {
      actionError = getErrorMessage(e)
    }
  }
</script>

<Section>
  {#if actionError}
    <Banner tint="danger" text={actionError} />
  {/if}

  <Toolbar overflow="menu" moreLabel={t('providers.detail.more_actions')}>
    <ToolbarItem pinned fill="md">
      <SearchField bind:value={query} placeholder={t('plugins.search.catalog')} />
    </ToolbarItem>
    <ToolbarItem priority={3} active={showUpdatesOnly}>
      <Toggle bind:checked={showUpdatesOnly} label={t('plugins.updates_only')} ariaLabel={t('plugins.updates_only')} />
    </ToolbarItem>
    <ToolbarSpacer />
    <ToolbarItem priority={1}>
      <Button
        buttonStyle="prominent"
        icon={{ name: 'add' }}
        text={t('plugins.repo.add')}
        onclick={openAddRepo}
      />
    </ToolbarItem>
    <ToolbarItem priority={2}>
      <Button
        buttonStyle="neutral"
        icon={{ name: 'sync' }}
        text={refreshing ? t('plugins.repo.refreshing') : t('plugins.repo.refresh')}
        disabled={refreshing}
        onclick={() => void refreshRepos()}
        ariaLabel={t('plugins.repo.refresh')}
      />
    </ToolbarItem>
  </Toolbar>
</Section>

{#if showUpdatesBanner}
  <Card title={t('plugins.updates_available')}>
    <List>
      {#each availableUpdates as u (u.plugin_id)}
        {@const approved = installedDigest(u)}
        <ListRow title={u.plugin_id} subtitle={`${u.current} → ${u.latest}`}>
          {#snippet trailing()}
            <Button buttonStyle="neutral" size="sm" disabled={!approved} onclick={() => { if (approved) void installEntry(u.repo_id, u.path, approved) }}>{t('common.actions.update')}</Button>
          {/snippet}
        </ListRow>
      {/each}
    </List>
  </Card>
{/if}

{#if repos.length === 0}
  {#snippet addRepoAction()}
    <Button buttonStyle="prominent" icon={{ name: 'add' }} onclick={openAddRepo}>{t('plugins.repo.add')}</Button>
  {/snippet}
  <ContentUnavailable
    icon="download"
    title={t('plugins.repo.empty')}
    caption={t('plugins.repo.add_short')}
    action={addRepoAction}
  />
{:else if visibleRepos.length === 0}
  <ContentUnavailable title={t('plugins.catalog.no_filter_match')} icon="search" />
{:else}
  {#each visibleRepos as entry (entry.repo.id)}
    {#snippet repoActions()}
      {#if entry.repo.builtin}<Chip text={t('plugins.repo.builtin')} tint="neutral" size="sm" />{/if}
      <Button
        buttonStyle="text"
        icon={{ name: 'info' }}
        size="sm"
        onclick={() => openRepoDetails(entry)}
        ariaLabel={t('plugins.repo.details')}
      />
      {#if !entry.repo.builtin}
        <DeleteButton size="sm" label={t('plugins.repo.remove')} onclick={(e) => void removeRepo(entry, e.currentTarget as HTMLElement)} />
      {/if}
    {/snippet}
    <Section title={entry.repo.title || entry.repo.id} subtitle={entry.repo.description || undefined} actions={repoActions}>
        {#if entry.error}
          <Banner tint="danger" text={entry.error} />
        {:else if entry.files.length === 0}
          <ContentUnavailable title={t('plugins.repo.no_plugins')} icon="extension" />
        {:else}
          <List>
            {#each entry.files as f (storeKey(f.repo_id, f.path))}
              {@const plugin = pluginByOrigin.get(storeKey(f.repo_id, f.path)) ?? null}
              {@const key = storeKey(f.repo_id, f.path)}
              {#if plugin}
                {@const update = findUpdate(plugin)}
                {@const review = update?.new_permissions ?? plugin.permissions}
                <PluginCard
                  title={f.display_name || f.path}
                  version={`v${plugin.version}`}
                  description={f.description}
                  iconUrl={plugin.icon_url}
                  subsystems={plugin.subsystems}
                  mode="installed"
                  permissions={plugin.permissions}
                  {review}
                  diff={diffPermissions(plugin.permissions, review)}
                  disabled={plugin.disabled}
                  toggling={pluginState.isToggling(plugin.id)}
                  updateVersion={update ? `v${update.latest}` : undefined}
                  onUpdate={() => void installEntry(f.repo_id, f.path, review.digest)}
                  onToggle={(enabled) => pluginState.togglePlugin(plugin, enabled)}
                  onaction={(id, anchor) => pluginState.handleAction(plugin, id, anchor)}
                />
              {:else}
                <PluginCard
                  title={f.display_name || f.path}
                  version={f.version ? `v${f.version}` : ''}
                  description={f.description}
                  iconUrl={f.icon_url}
                  subsystems={f.subsystems}
                  mode="uninstalled"
                  permissions={f.permissions}
                  review={f.permissions}
                  installing={installingPath === key}
                  onInstall={() => { if (f.permissions) void installEntry(f.repo_id, f.path, f.permissions.digest) }}
                />
              {/if}
              {#if f.error}<Text tint="danger" variant="caption">{f.error}</Text>{/if}
            {/each}
          </List>
        {/if}
    </Section>
  {/each}
{/if}


