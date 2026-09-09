<script lang="ts">
  import { api } from '../../lib/api'
  import { modal } from '../../lib/modal.svelte'
  import { getErrorMessage } from '../../lib/errors'
  import SearchField from '../ui/SearchField.svelte'
  import EmptyState from '../EmptyState.svelte'
  import PluginCard from './PluginCard.svelte'
  import PluginDetails from './PluginDetails.svelte'
  import { createPluginState } from './plugin-state.svelte'
  import type { Plugin, PluginRepo, PluginUpdate, StoreFile } from '../../lib/types'

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
  let uploading = $state(false)
  let uploadNotice = $state('')

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
    findRepoPath
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

  async function handleCatalogAction(plugin: Plugin, file: StoreFile, id: string): Promise<void> {
    if (id === 'update' || id === 'reinstall') {
      await installEntry(file.repo_id, file.path)
      return
    }
    await pluginState.handleAction(plugin, id)
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

  async function removeRepo(id: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Remove repository',
      message: 'Remove this repository from the store? Installed plugins stay installed.',
      severity: 'medium',
      confirmText: 'Remove',
      cancelText: 'Cancel',
      danger: false
    })
    if (!confirmed) return
    try {
      await api.repos.remove(id)
      await onReload()
    } catch (e) {
      actionError = getErrorMessage(e)
    }
  }

  async function uploadFile(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    uploading = true
    actionError = ''
    try {
      const text = await file.text()
      uploadNotice = text.slice(0, 80)
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

<div class="toolbar">
  <div class="toolbar-search">
    <SearchField bind:value={query} placeholder="Search catalog..." />
  </div>
  <div class="toolbar-actions">
    <button
      type="button"
      class="btn btn-secondary chip"
      class:active={showUpdatesOnly}
      aria-pressed={showUpdatesOnly}
      onclick={() => {
        showUpdatesOnly = !showUpdatesOnly
      }}
    >
      Updates only{#if availableUpdates.length > 0} ({availableUpdates.length}){/if}
    </button>
    <label class="btn btn-secondary file-label">
      {uploading ? 'Uploading…' : 'Upload file'}
      <input type="file" accept=".lua" onchange={uploadFile} disabled={uploading} hidden />
    </label>
    <button class="btn btn-primary" onclick={() => {
      showAddRepo = !showAddRepo
    }}>
      <span class="icon">add</span>
      Add repository
    </button>
  </div>
</div>

{#if actionError}
  <div class="error-msg">{actionError}</div>
{/if}

{#if showAddRepo}
  <div class="card form-card">
    <div class="form-group">
      <label for="repo-url">Repository or index URL</label>
      <input id="repo-url" type="text" bind:value={repoUrl} placeholder="https://github.com/octocat/llm-router-plugins" />
      <div class="hint">Paste a repository URL or a direct index.json URL.</div>
    </div>
    <div class="form-actions">
      <button class="btn btn-secondary" onclick={() => {
        showAddRepo = false
      }}>Cancel</button>
      <button class="btn btn-primary" disabled={adding} onclick={addRepo}>Add</button>
    </div>
  </div>
{/if}

{#if availableUpdates.length > 0}
  <div class="card updates-card">
    <h2>Updates available</h2>
    {#each availableUpdates as u}
      <div class="update-row">
        <span>{u.plugin_id}: {u.current} → {u.latest}</span>
        <button class="btn btn-secondary" onclick={() => installEntry(u.repo_id, u.path)}>Update</button>
      </div>
    {/each}
  </div>
{/if}

{#if repos.length === 0}
  <EmptyState
    icon="download"
    message="No repositories added yet"
    hint="Add a plugin repository or upload a .lua file to install a plugin."
    buttonText="Add repository"
    buttonIcon="add"
    onButtonClick={() => {
      showAddRepo = true
    }}
  />
{:else if visibleRepos.length === 0}
  <div class="empty">No plugins match the current filter.</div>
{:else}
  {#each visibleRepos as entry (entry.repo.id)}
    <div class="card repo-card">
      <div class="repo-header">
        <div>
          <h2>{entry.repo.id}</h2>
          <div class="muted">{entry.repo.url}</div>
        </div>
        <div class="repo-badges">
          {#if entry.repo.builtin}<span class="badge">Built-in</span>{/if}
          {#if !entry.repo.builtin}
            <button class="btn-icon" onclick={() => removeRepo(entry.repo.id)} aria-label="Remove repository">
              <span class="icon">delete</span>
            </button>
          {/if}
        </div>
      </div>
      {#if entry.error}
        <div class="error-msg">{entry.error}</div>
      {:else if entry.files.length === 0}
        <div class="empty">No plugins in this repository.</div>
      {:else}
        <div class="file-list">
          {#each entry.files as f (`${f.repo_id}/${f.path}`)}
            {@const plugin = pluginByOrigin.get(`${f.repo_id}/${f.path}`) ?? null}
            {@const key = `${f.repo_id}/${f.path}`}
            {#if plugin}
              <PluginCard
                title={f.display_name || f.path}
                meta={f.version ? `v${f.version}` : ''}
                badges={[
                  ...(f.unsafe ? [{ text: 'Unrestricted network', kind: 'badge-red' as const }] : []),
                  ...(!plugin.enabled ? [{ text: 'Disabled', kind: '' as const }] : []),
                  ...(f.update_available
                    ? [{ text: `Update: v${f.installed_version} → v${f.version}`, kind: 'badge-green' as const }]
                    : [{ text: 'Installed', kind: '' as const }])
                ]}
                subtitle={f.path}
                description={f.description}
                typeKeys={plugin.type_keys}
                mode="installed"
                actions={pluginState.buildActions(plugin)}
                onaction={(id) => handleCatalogAction(plugin, f, id)}
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
            {:else}
              <PluginCard
                title={f.display_name || f.path}
                meta={f.version ? `v${f.version}` : ''}
                badges={[...(f.unsafe ? [{ text: 'Unrestricted network', kind: 'badge-red' as const }] : [])]}
                subtitle={f.path}
                description={f.description}
                mode="uninstalled"
                installLabel="Install"
                installing={installingPath === key}
                onInstall={() => installEntry(f.repo_id, f.path)}
              />
            {/if}
            {#if f.error}<div class="error-msg">{f.error}</div>{/if}
          {/each}
        </div>
      {/if}
    </div>
  {/each}
{/if}

{#if uploadNotice}
  <div class="muted">Uploaded: {uploadNotice}…</div>
{/if}

<style>
  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 16px;
    margin-bottom: 16px;
    flex-wrap: wrap;
  }
  .toolbar-search {
    flex: 1;
    min-width: 220px;
  }
  .toolbar-search :global(.search-field) {
    margin-bottom: 0;
  }
  .toolbar-actions {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }
  .chip.active {
    background: var(--color-nav-active);
    color: var(--color-text);
  }
  .file-label {
    cursor: pointer;
  }
  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 16px;
    padding: 20px;
    margin-bottom: 16px;
  }
  .card h2 {
    font-size: 15px;
    margin: 0 0 12px 0;
  }
  .form-card {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .updates-card {
    margin-bottom: 16px;
  }
  .update-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 0;
  }
  .repo-card {
    margin-bottom: 16px;
  }
  .repo-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
  }
  .repo-header h2 {
    margin: 0;
  }
  .repo-badges {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .file-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .muted {
    font-size: 13px;
    color: var(--color-text-soft);
  }
  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .form-group label {
    font-size: 13px;
    font-weight: 500;
  }
  .form-actions {
    display: flex;
    gap: 8px;
  }
</style>
