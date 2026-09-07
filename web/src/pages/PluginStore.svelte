<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import type { PluginRepo, StoreFile, PluginUpdate } from '../lib/types'

  let storeRepos = $state<Array<{ repo: PluginRepo; files: StoreFile[]; error: string }>>([])
  let updates = $state<PluginUpdate[]>([])
  let loading = $state(true)
  let error = $state('')

  let showAddRepo = $state(false)
  let repoKind = $state<'github' | 'generic-index'>('github')
  let owner = $state('')
  let repoName = $state('')
  let indexUrl = $state('')
  let adding = $state(false)

  let uploadSource = $state('')
  let uploading = $state(false)

  onMount(async (): Promise<void> => {
    await reload()
  })

  async function reload(): Promise<void> {
    loading = true
    error = ''
    try {
      const [search, updateList] = await Promise.all([
        api.store.search().catch(() => ({ repos: [] })),
        api.store.updates().catch(() => ({ updates: [] })),
      ])
      storeRepos = search.repos ?? []
      updates = updateList.updates ?? []
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function addRepo(): Promise<void> {
    adding = true
    error = ''
    try {
      if (repoKind === 'github') {
        await api.repos.addGitHub(owner.trim(), repoName.trim())
      } else {
        await api.repos.addGeneric(indexUrl.trim())
      }
      showAddRepo = false
      owner = ''
      repoName = ''
      indexUrl = ''
      await reload()
    } catch (e) {
      error = getErrorMessage(e)
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
      await reload()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function installFile(repoId: string, path: string): Promise<void> {
    try {
      await api.plugins.installFromRepo(repoId, path)
      await reload()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function uploadFile(e: Event): Promise<void> {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    uploading = true
    error = ''
    try {
      const text = await file.text()
      uploadSource = text.slice(0, 80)
      await api.plugins.installFile(text)
      await reload()
    } catch (err) {
      error = getErrorMessage(err)
    } finally {
      uploading = false
      input.value = ''
    }
  }
</script>

<div class="page-header">
  <div>
    <h1>Plugin Store</h1>
    <p>Install single-file Lua plugins from repositories or manual upload.</p>
  </div>
  <div class="header-actions">
    <label class="btn btn-secondary file-label">
      {uploading ? 'Uploading…' : 'Upload file'}
      <input type="file" accept=".lua" onchange={uploadFile} disabled={uploading} hidden />
    </label>
    <button class="btn btn-primary" onclick={() => { showAddRepo = !showAddRepo }}>
      <span class="icon">add</span>
      Add repository
    </button>
  </div>
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if showAddRepo}
  <div class="card">
    <div class="form-group">
      <label for="repo-kind">Kind</label>
      <select id="repo-kind" bind:value={repoKind}>
        <option value="github">GitHub</option>
        <option value="generic-index">Generic index URL</option>
      </select>
    </div>
    {#if repoKind === 'github'}
      <div class="form-row">
        <div class="form-group">
          <label for="repo-owner">Owner</label>
          <input id="repo-owner" type="text" bind:value={owner} placeholder="octocat" />
        </div>
        <div class="form-group">
          <label for="repo-name">Repository</label>
          <input id="repo-name" type="text" bind:value={repoName} placeholder="llm-router-plugins" />
        </div>
      </div>
    {:else}
      <div class="form-group">
        <label for="index-url">Index URL</label>
        <input id="index-url" type="text" bind:value={indexUrl} placeholder="https://example.com/plugins/index.json" />
      </div>
    {/if}
    <div class="form-actions">
      <button class="btn btn-secondary" onclick={() => { showAddRepo = false }}>Cancel</button>
      <button class="btn btn-primary" disabled={adding} onclick={addRepo}>Add</button>
    </div>
  </div>
{/if}

{#if updates.filter((u) => u.update_available).length > 0}
  <div class="card">
    <h2>Updates available</h2>
    {#each updates.filter((u) => u.update_available) as u}
      <div class="update-row">
        <span>{u.plugin_id}: {u.current} → {u.latest}</span>
        <button class="btn btn-secondary" onclick={() => installFile(u.repo_id, u.path)}>Update</button>
      </div>
    {/each}
  </div>
{/if}

{#if loading}
  <div class="empty">Loading…</div>
{:else if storeRepos.length === 0}
  <div class="empty">No repositories added yet.</div>
{:else}
  {#each storeRepos as entry}
    <div class="card">
      <div class="repo-header">
        <h2>{entry.repo.id}</h2>
        <button class="btn-icon" onclick={() => removeRepo(entry.repo.id)} aria-label="Remove repository">
          <span class="icon">delete</span>
        </button>
      </div>
      {#if entry.error}
        <div class="error-msg">{entry.error}</div>
      {:else if entry.files.length === 0}
        <div class="empty">No plugins in this repository.</div>
      {:else}
        <div class="file-list">
          {#each entry.files as f}
            <div class="file-item">
              <div class="file-info">
                <strong>{f.display_name || f.path}</strong>
                {#if f.version}<span class="muted">v{f.version}</span>{/if}
                {#if f.unsafe}<span class="badge badge-red">Unrestricted network</span>{/if}
                {#if f.update_available}<span class="badge badge-green">Update: v{f.installed_version} → v{f.version}</span>
                {:else if f.installed}<span class="badge">Installed</span>{/if}
                {#if f.description}<div class="muted">{f.description}</div>{/if}
                {#if f.error}<div class="error-msg">{f.error}</div>{/if}
              </div>
              <button class="btn btn-secondary" onclick={() => installFile(f.repo_id, f.path)}>
                {f.installed ? 'Reinstall' : 'Install'}
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/each}
{/if}

{#if uploadSource}
  <div class="muted">Uploaded: {uploadSource}…</div>
{/if}

<style>
  .header-actions {
    display: flex;
    gap: 8px;
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
  .repo-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .repo-header h2 {
    margin: 0;
  }
  .file-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .file-item {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--color-outline-soft);
    border-radius: 8px;
  }
  .file-info {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .muted {
    font-size: 13px;
    color: var(--color-text-soft);
  }
  .update-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 0;
  }
  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
  }
  .form-group label {
    font-size: 13px;
    font-weight: 500;
  }
  .form-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  .form-actions {
    display: flex;
    gap: 8px;
  }
</style>
