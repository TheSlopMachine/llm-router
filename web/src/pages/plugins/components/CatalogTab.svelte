<script lang="ts">
  import { Button, FloatingView, HStack, VStack, Text, Switch, SearchField, TextEdit, List, SectionCard, Chip, Spacer } from '$ui'
  import EmptyState from '../../../components/EmptyState.svelte'
  import PluginCard from './PluginCard.svelte'
  import PluginDetailsModal from './PluginDetailsModal.svelte'
  import RepoDetailsModal from './RepoDetailsModal.svelte'
  import { createPluginState } from './plugin-state.svelte'
  import { factsFromFile } from './plugin-facts'
  import { diffAllowHosts } from './plugin-permission-diff'
  import { confirmInstall, confirmUpdate } from './install-confirm'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '$lib/types'
  import { api } from '$lib/api'
  import { modal } from '$lib/modal.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'

  interface RepoEntry {
    repo: PluginRepo
    files: StoreFile[]
    error: string
  }

  let {
    repos,
    plugins,
    updates,
    onReload
  } = $props<{
    repos: RepoEntry[]
    plugins: Plugin[]
    updates: PluginUpdate[]
    onReload: () => Promise<void>
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
    if (plugin.origin && !plugin.origin.manual && plugin.origin.repo_id && plugin.origin.path) {
      if (!repos.some((entry: RepoEntry) => entry.repo.id === plugin.origin.repo_id)) {
        return null
      }
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
    findRepoPath,
    findStoreFile: (plugin: Plugin) => {
      if (plugin.origin && !plugin.origin.manual && plugin.origin.repo_id && plugin.origin.path) {
        return fileByOrigin.get(`${plugin.origin.repo_id}/${plugin.origin.path}`) ?? null
      }
      return null
    }
  })

  let pluginByOrigin = $derived.by(() => {
    const map = new Map<string, Plugin>()
    for (const p of plugins) {
      if (p.origin && !p.origin.manual && p.origin.repo_id && p.origin.path) {
        map.set(`${p.origin.repo_id}/${p.origin.path}`, p)
      }
    }
    return map
  })

  let availableUpdates = $derived(updates.filter((u: PluginUpdate) => u.update_available))

  let fileByOrigin = $derived.by(() => {
    const map = new Map<string, StoreFile>()
    for (const entry of repos) {
      for (const f of entry.files) {
        map.set(`${f.repo_id}/${f.path}`, f)
      }
    }
    return map
  })

  function matchesQuery(f: StoreFile): boolean {
    const q = query.trim().toLowerCase()
    if (!q) return true
    return (
      (f.display_name || '').toLowerCase().includes(q) ||
      f.path.toLowerCase().includes(q) ||
      (f.description || '').toLowerCase().includes(q)
    )
  }

  let visibleRepos = $derived.by(() => {
    return repos
      .map((entry: RepoEntry) => ({
        ...entry,
        files: entry.files.filter((f: StoreFile) => {
          if (showUpdatesOnly && !f.update_available) return false
          return matchesQuery(f)
        })
      }))
      .filter((entry: RepoEntry) => entry.error || entry.files.length > 0 || (!query.trim() && !showUpdatesOnly))
  })

  async function installEntry(repoId: string, path: string): Promise<void> {
    const key = `${repoId}/${path}`
    installingPath = key
    actionError = ''
    try {
      await api.plugins.installFromRepo(repoId, path)
      await onReload()
    } catch (e) {
      actionError = getErrorMessage(e)
    } finally {
      installingPath = null
    }
  }

  async function handleCatalogAction(plugin: Plugin, file: StoreFile, id: string, anchor?: HTMLElement): Promise<void> {
    if (id === 'update') {
      await confirmAndInstall(file, 'Update', plugin)
      return
    }
    if (id === 'reinstall') {
      await confirmAndInstall(file, 'Reinstall', plugin)
      return
    }
    await pluginState.handleAction(plugin, id, anchor)
  }

  async function confirmAndInstall(file: StoreFile, label: string, plugin?: Plugin): Promise<void> {
    if (plugin) {
      const update = findUpdate(plugin)
      const latest = update?.latest || file.version
      const diff = diffAllowHosts(plugin.allow_hosts ?? [], plugin.unsafe, file.allow_hosts ?? [], file.unsafe)
      const confirmed = await confirmUpdate(
        `${label} ${file.display_name || file.path}`,
        {
          displayName: file.display_name || file.path,
          current: plugin.version,
          latest,
          newHosts: [...(file.allow_hosts ?? [])],
          newUnsafe: file.unsafe,
          added: diff.added,
          removed: diff.removed,
          escalatesToUnsafe: diff.escalatesToUnsafe
        },
        label
      )
      if (!confirmed) return
      await installEntry(file.repo_id, file.path)
      return
    }
    const confirmed = await confirmInstall(
      `${label} ${file.display_name || file.path}`,
      factsFromFile(file),
      label
    )
    if (!confirmed) return
    await installEntry(file.repo_id, file.path)
  }

  function openFileDetails(file: StoreFile): void {
    modal.open({
      title: file.display_name || file.path,
      content: PluginDetailsModal,
      severity: 'medium',
      size: 'medium',
      props: { facts: factsFromFile(file) }
    })
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
      await api.repos.addRepo(repoUrl.trim())
      showAddRepo = false
      repoUrl = ''
      await onReload()
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
      await onReload()
    } catch (e) {
      actionError = getErrorMessage(e)
    } finally {
      removingRepo = false
    }
  }
</script>

<VStack gap={4}>
  {#if actionError}
    <Text tone="danger" size="sm">{actionError}</Text>
  {/if}

  <HStack align="center" gap={4}>
    <div style="flex: 1;">
      <SearchField bind:value={query} placeholder={t('Search catalog...')} />
    </div>
    <HStack align="center" gap={4}>
      <HStack align="center" gap={2}>
        <Text size="sm">{t('Updates only')}</Text>
        <Switch bind:checked={showUpdatesOnly} ariaLabel={t('Updates only')} />
      </HStack>
      <Button
        style="prominent"
        icon={{ name: 'add' }}
        onclick={() => { showAddRepo = !showAddRepo }}
      >{t('Add repository')}</Button>
    </HStack>
  </HStack>

  {#if showAddRepo}
    <SectionCard title="Add repository" description="Paste a repository URL or a direct index.json URL.">
      <VStack gap={4}>
        <VStack gap={1}>
          <Text size="sm" weight="medium" tag="label" for="repo-url">{t('Repository or index URL')}</Text>
          <TextEdit id="repo-url" bind:value={repoUrl} hint="https://github.com/..." />
        </VStack>
        <HStack gap={2} justify="end">
          <Button onclick={() => { showAddRepo = false }}>{t('Cancel')}</Button>
          <Button style="prominent" onclick={addRepo} disabled={adding || !repoUrl.trim()}>{t('Add')}</Button>
        </HStack>
      </VStack>
    </SectionCard>
  {/if}

  {#if availableUpdates.length > 0 && !showUpdatesOnly}
    <SectionCard title="Updates available">
      <List>
        {#each availableUpdates as u}
          {@const file = fileByOrigin.get(`${u.repo_id}/${u.path}`) ?? null}
          {@const installed = plugins.find((p: Plugin) => p.id === u.plugin_id) ?? null}
          <div style="padding: var(--space-3) var(--space-4);">
            <HStack align="center" gap={3}>
              <Text size="sm">{u.plugin_id}: {u.current} → {u.latest}</Text>
              <Spacer />
              {#if file}
                <Button style="none" size="small" onclick={() => confirmAndInstall(file, 'Update', installed ?? undefined)}>{t('Update')}</Button>
              {:else}
                <Button style="none" size="small" onclick={() => installEntry(u.repo_id, u.path)}>{t('Update')}</Button>
              {/if}
            </HStack>
          </div>
        {/each}
      </List>
    </SectionCard>
  {/if}

  {#if repos.length === 0}
    {#snippet addRepoAction()}
      <Button style="prominent" icon={{ name: 'add' }} onclick={() => { showAddRepo = true }}>{t('Add repository')}</Button>
    {/snippet}
    <EmptyState
      icon="download"
      title={t('No repositories added yet')}
      caption={t('Add a plugin repository to install a plugin.')}
      action={addRepoAction}
    />
  {:else if visibleRepos.length === 0}
    <Text tone="soft" align="center">{t('No plugins match the current filter.')}</Text>
  {:else}
    {#each visibleRepos as entry (entry.repo.id)}
      <VStack gap={3}>
        <HStack align="center" gap={3}>
          <VStack gap={0} grow>
            <Text tag="h2" size="md" weight="bold">{entry.repo.title || entry.repo.id}</Text>
            {#if entry.repo.description}<Text size="xs" tone="soft">{entry.repo.description}</Text>{/if}
          </VStack>
          <HStack gap={2}>
            {#if entry.repo.builtin}<Chip text={t('Built-in')} color="chip-neutral" size="small" />{/if}
            <Button
              style="text"
              icon={{ name: 'info' }}
              size="small"
              onclick={() => openRepoDetails(entry)}
              ariaLabel={t('Repository details')}
            />
            {#if !entry.repo.builtin}
              <Button
                style="text"
                tint="#dc2626"
                icon={{ name: 'delete' }}
                size="small"
                onclick={(e) => openRemoveRepo(entry, e.currentTarget as HTMLElement)}
                ariaLabel={t('Remove repository')}
              />
            {/if}
          </HStack>
        </HStack>

        {#if entry.error}
          <Text tone="danger" size="sm">{entry.error}</Text>
        {:else if entry.files.length === 0}
          <Text tone="soft" size="sm" align="center" class="empty">{t('No plugins in this repository.')}</Text>
        {:else}
          <List>
            {#each entry.files as f (`${f.repo_id}/${f.path}`)}
              {@const plugin = pluginByOrigin.get(`${f.repo_id}/${f.path}`) ?? null}
              {@const key = `${f.repo_id}/${f.path}`}
              {#if plugin}
                {@const update = findUpdate(plugin)}
                <PluginCard
                  title={f.display_name || f.path}
                  version={f.version ? `v${f.version}` : ''}
                  origin={entry.repo.title || entry.repo.id}
                  description={f.description}
                  mode="installed"
                  unsafe={f.unsafe}
                  hasUpdate={!!update}
                  onDetails={() => pluginState.openDetails(plugin)}
                  onaction={(id, anchor) => handleCatalogAction(plugin, f, id, anchor)}
                />
              {:else}
                <PluginCard
                  title={f.display_name || f.path}
                  version={f.version ? `v${f.version}` : ''}
                  origin={entry.repo.title || entry.repo.id}
                  description={f.description}
                  mode="uninstalled"
                  unsafe={f.unsafe}
                  installing={installingPath === key}
                  onDetails={() => openFileDetails(f)}
                  onInstall={() => confirmAndInstall(f, 'Install')}
                />
              {/if}
              {#if f.error}<Text tone="danger" size="xs">{f.error}</Text>{/if}
            {/each}
          </List>
        {/if}
      </VStack>
    {/each}
  {/if}

  <FloatingView
    open={Boolean(removeRepoTarget)}
    anchor={removeRepoAnchor}
    onclose={() => { removeRepoTarget = null }}
    label={t('Remove repository')}
  >
    {#snippet children({ close })}
      <VStack gap={3} style="max-width: 280px;">
        <VStack gap={1}>
          <Text weight="medium" size="base">{t('Remove repository')}</Text>
          <Text size="sm" tone="soft">
            {t('Remove this repository from the store? Installed plugins stay installed.')}
          </Text>
        </VStack>
        <HStack justify="end" gap={2}>
          <Button size="small" style="text" onclick={close} disabled={removingRepo}>{t('Cancel')}</Button>
          <Button
            size="small"
            style="prominent"
            tint="#dc2626"
            disabled={removingRepo}
            onclick={confirmRemoveRepo}
          >
            {removingRepo ? t('Removing…') : t('Remove')}
          </Button>
        </HStack>
      </VStack>
    {/snippet}
  </FloatingView>

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


