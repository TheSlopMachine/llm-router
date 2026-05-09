<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal'
  import EmptyState from './EmptyState.svelte'
  import type { Credential, Provider } from '../lib/types'

  let credentials: Credential[] = []
  let providers: Provider[] = []
  let loading: boolean = true
  let error: string = ''

  onMount(load)

  async function load(): Promise<void> {
    loading = true
    error = ''
    try {
      const [c, p] = await Promise.all([
        api.credentials.list(),
        api.providers.list(),
      ])
      credentials = c || []
      providers = p || []
    } catch (e) {
      error = (e as Error).message
    } finally {
      loading = false
    }
  }

  async function remove(id: string, label: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Delete credential',
      message: `Are you sure you want to delete credential "${label}"? This action cannot be undone.`,
      severity: 'medium',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true
    })
    
    if (!confirmed) return
    
    try {
      await api.credentials.delete(id)
      await load()
    } catch (e) {
      error = (e as Error).message
    }
  }

  function fmt(d: string): string { return new Date(d).toISOString().slice(0, 10) }
  function fmtExpiry(d: string | null): string {
    if (!d) return '—'
    return new Date(d).toISOString().slice(0, 16).replace('T', ' ')
  }
</script>

<div class="page-header">
  <div>
    <h1>Credentials</h1>
    <p>Manage authentication credentials for all providers.</p>
  </div>
  {#if credentials.length > 0}
    <button class="btn btn-primary" on:click={() => window.location.hash = '#/providers'}>
      <span class="icon">cloud</span>
      Configure Providers
    </button>
  {/if}
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if loading}
  <div class="loading">Loading credentials...</div>
{:else if credentials.length === 0}
  <EmptyState 
    icon="lock"
    message="No credentials yet"
    hint="Credentials are added through provider configuration."
    buttonText="Configure Providers"
    buttonIcon="cloud"
    onButtonClick={() => window.location.hash = '#/providers'}
  />
{:else}
  <div class="card">
    <div class="card-header"><h2>Credentials</h2></div>
    <table>
      <thead>
        <tr><th>Provider</th><th>Label</th><th>Status</th><th>Expires</th><th>Updated</th><th></th></tr>
      </thead>
      <tbody>
        {#each credentials as c}
          <tr>
            <td>{c.provider_name}</td>
            <td>{c.label}</td>
            <td>
              {#if c.is_expired}
                <span class="badge badge-red">Expired</span>
              {:else if c.expires_at}
                <span class="badge badge-yellow">Active</span>
              {:else}
                <span class="badge badge-green">Active</span>
              {/if}
            </td>
            <td>{fmtExpiry(c.expires_at)}</td>
            <td>{fmt(c.updated_at)}</td>
            <td>
              <button class="btn btn-danger" on:click={() => remove(c.id, c.label)}>Delete</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 24px;
    gap: 24px;
  }

  .page-header h1 {
    font-size: 24px;
    font-weight: 400;
    margin: 0 0 4px 0;
  }

  .page-header p {
    color: var(--color-text-soft);
    font-size: 14px;
    margin: 0;
  }

  .loading {
    text-align: center;
    padding: 48px;
    color: var(--color-text-soft);
  }
</style>
