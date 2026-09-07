<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import type { Plugin } from '../lib/types'

  let plugins = $state<Plugin[]>([])
  let loading = $state(true)
  let error = $state('')
  let expanded = $state<string | null>(null)
  let details = $state<Record<string, { logs: Array<{ at: string; message: string }>; crashes: Array<{ at: string; type_key: string; cause: string }> }>>({})

  onMount(async (): Promise<void> => {
    await reload()
  })

  async function reload(): Promise<void> {
    loading = true
    error = ''
    try {
      plugins = await api.plugins.list()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function toggleDetails(id: string): Promise<void> {
    if (expanded === id) {
      expanded = null
      return
    }
    expanded = id
    if (!details[id]) {
      try {
        const [logs, crashes] = await Promise.all([api.plugins.logs(id), api.plugins.crashes(id)])
        details[id] = { logs, crashes }
      } catch (e) {
        error = getErrorMessage(e)
      }
    }
  }

  async function setEnabled(plugin: Plugin, enabled: boolean): Promise<void> {
    try {
      if (enabled) {
        await api.plugins.enable(plugin.id)
      } else {
        await api.plugins.disable(plugin.id)
      }
      await reload()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function rollback(plugin: Plugin): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Roll back plugin',
      message: `Roll "${plugin.display_name}" back to the previous version?`,
      severity: 'medium',
      confirmText: 'Roll back',
      cancelText: 'Cancel',
      danger: false
    })
    if (!confirmed) return
    try {
      await api.plugins.rollback(plugin.id)
      await reload()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function removePlugin(plugin: Plugin): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Delete plugin',
      message: `Delete "${plugin.display_name}"? Providers using its types will stop working.`,
      severity: 'high',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true
    })
    if (!confirmed) return
    try {
      await api.plugins.remove(plugin.id)
      await reload()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }
</script>

<div class="page-header">
  <div>
    <h1>Plugins</h1>
    <p>Installed Lua plugins and their type keys.</p>
  </div>
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if loading}
  <div class="empty">Loading…</div>
{:else if plugins.length === 0}
  <div class="empty">No plugins installed.</div>
{:else}
  <div class="plugin-list">
    {#each plugins as plugin}
      <div class="card">
        <div class="plugin-header">
          <div>
            <strong>{plugin.display_name}</strong>
            <span class="muted">v{plugin.version} · {plugin.author}</span>
            {#if plugin.unsafe}
              <span class="badge badge-red">Unrestricted network</span>
            {/if}
            {#if !plugin.enabled}
              <span class="badge">Disabled</span>
            {/if}
          </div>
          <div class="plugin-actions">
            <button class="btn btn-secondary" onclick={() => toggleDetails(plugin.id)}>Details</button>
            {#if plugin.enabled}
              <button class="btn btn-secondary" onclick={() => setEnabled(plugin, false)}>Disable</button>
            {:else}
              <button class="btn btn-secondary" onclick={() => setEnabled(plugin, true)}>Enable</button>
            {/if}
            {#if plugin.history_count > 0}
              <button class="btn btn-secondary" onclick={() => rollback(plugin)}>Roll back</button>
            {/if}
            <button class="btn btn-danger" onclick={() => removePlugin(plugin)}>Delete</button>
          </div>
        </div>
        <div class="muted">{plugin.id}</div>
        {#if plugin.description}<div class="muted">{plugin.description}</div>{/if}
        <div class="type-keys">
          {#each plugin.type_keys as key}
            <span class="badge">{key}</span>
          {/each}
        </div>
        {#if expanded === plugin.id}
          <div class="details">
            <h3>Allow hosts</h3>
            <div class="muted">{plugin.allow_hosts.join(', ')}</div>
            <h3>Recent crashes</h3>
            {#if (details[plugin.id]?.crashes ?? []).length === 0}
              <div class="muted">None recorded.</div>
            {:else}
              {#each details[plugin.id].crashes as crash}
                <div class="log-line"><span class="muted">{crash.at} [{crash.type_key}]</span> {crash.cause}</div>
              {/each}
            {/if}
            <h3>Recent log output</h3>
            {#if (details[plugin.id]?.logs ?? []).length === 0}
              <div class="muted">None recorded.</div>
            {:else}
              {#each details[plugin.id].logs as log}
                <div class="log-line"><span class="muted">{log.at}</span> {log.message}</div>
              {/each}
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .plugin-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 16px;
    padding: 20px;
  }
  .plugin-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
  }
  .plugin-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .muted {
    font-size: 13px;
    color: var(--color-text-soft);
    margin-top: 4px;
  }
  .type-keys {
    display: flex;
    gap: 6px;
    margin-top: 8px;
    flex-wrap: wrap;
  }
  .details {
    margin-top: 12px;
    border-top: 1px solid var(--color-border);
    padding-top: 12px;
  }
  .details h3 {
    font-size: 13px;
    margin: 12px 0 4px 0;
  }
  .log-line {
    font-size: 13px;
    padding: 4px 0;
  }
</style>
