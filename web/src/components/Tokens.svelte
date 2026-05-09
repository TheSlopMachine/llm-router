<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal'
  import TokenWizard from './wizards/TokenWizard.svelte'
  import EmptyState from './EmptyState.svelte'
  import type { Token, Provider } from '../lib/types'

  let tokens: Token[] = []
  let providers: Provider[] = []
  let loading: boolean = true
  let error: string = ''
  let newTokenSecret: string | null = null
  let tokenUsage: Record<string, TokenUsageInfo> = {}

  onMount(load)

  async function load(): Promise<void> {
    loading = true
    try {
      const [t, p, u] = await Promise.all([
        api.tokens.list(), 
        api.providers.list(),
        api.tokens.usage()
      ])
      tokens = t || []
      providers = p || []
      tokenUsage = u || {}
    } catch (e) {
      error = (e as Error).message
    } finally {
      loading = false
    }
  }

  function openCreate(): void {
    newTokenSecret = null
    error = ''
    
    modal.open({
      title: 'New token',
      content: TokenWizard,
      severity: 'medium',
      size: 'large',
      props: {
        providers,
        editingToken: null,
        onComplete: async (result: { token?: string }) => {
          if (result.token) {
            newTokenSecret = result.token
          }
          modal.close()
          await load()
        }
      }
    })
  }

  async function openEdit(token: Token): Promise<void> {
    newTokenSecret = null
    error = ''
    
    modal.open({
      title: 'Edit token',
      content: TokenWizard,
      severity: 'medium',
      size: 'large',
      props: {
        providers,
        editingToken: token,
        onComplete: async () => {
          modal.close()
          await load()
        }
      }
    })
  }

  async function remove(id: string, name: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Revoke token',
      message: `Are you sure you want to revoke token "${name}"? This action cannot be undone.`,
      severity: 'medium',
      confirmText: 'Revoke',
      cancelText: 'Cancel',
      danger: true
    })
    
    if (!confirmed) return
    
    try {
      await api.tokens.delete(id)
      await load()
    } catch (e) {
      error = (e as Error).message
    }
  }

  function fmt(d: string): string { return new Date(d).toISOString().slice(0, 10) }
  function shortId(id: string): string { return id.slice(0, 12) + '…' }
  function getUsage(tokenId: string): number { 
    return tokenUsage[tokenId]?.requests || 0 
  }

  function formatRelativeTime(isoString: string | undefined): string {
    if (!isoString) return '—'
    
    const date = new Date(isoString)
    const now = new Date()
    const diffMs = now.getTime() - date.getTime()
    const diffSec = Math.floor(diffMs / 1000)
    const diffMin = Math.floor(diffSec / 60)
    const diffHour = Math.floor(diffMin / 60)
    const diffDay = Math.floor(diffHour / 24)
    
    if (diffSec < 60) return 'Just now'
    if (diffMin < 60) return `${diffMin} minute${diffMin !== 1 ? 's' : ''} ago`
    if (diffHour < 24) return `${diffHour} hour${diffHour !== 1 ? 's' : ''} ago`
    if (diffDay < 30) return `${diffDay} day${diffDay !== 1 ? 's' : ''} ago`
    if (diffDay < 365) {
      const months = Math.floor(diffDay / 30)
      return `${months} month${months !== 1 ? 's' : ''} ago`
    }
    const years = Math.floor(diffDay / 365)
    return `${years} year${years !== 1 ? 's' : ''} ago`
  }

  function getLastUsed(tokenId: string): string {
    return formatRelativeTime(tokenUsage[tokenId]?.last_used)
  }
</script>

<div class="page-header">
  <div>
    <h1>Tokens</h1>
    <p>Router tokens for the <code>/v1</code> API. Each token enforces its own model allowlist.</p>
  </div>
  {#if tokens.length > 0}
    <button class="btn btn-primary" on:click={openCreate}>
      <span class="icon">add</span>
      New Token
    </button>
  {/if}
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if newTokenSecret}
  <div class="success-msg">
    Token created. Copy it now — it will not be shown again:<br />
    <span class="mono secret">{newTokenSecret}</span>
  </div>
{/if}

{#if loading}
  <div class="loading">Loading tokens...</div>
{:else if tokens.length === 0}
  <EmptyState 
    icon="key"
    message="No tokens yet"
    hint="Create a token to access the /v1 API with model-specific permissions."
    buttonText="Create Your First Token"
    buttonIcon="add"
    onButtonClick={openCreate}
  />
{:else}
  <div class="card">
    <div class="card-header"><h2>Tokens</h2></div>
    <table>
      <thead>
        <tr><th>Name</th><th>ID</th><th>Created</th><th>Last Used</th><th>API calls</th><th></th></tr>
      </thead>
      <tbody>
        {#each tokens as t}
          <tr>
            <td>{t.name}</td>
            <td class="mono">{shortId(t.id)}</td>
            <td>{fmt(t.created_at)}</td>
            <td>{getLastUsed(t.id)}</td>
            <td>{getUsage(t.id).toLocaleString()}</td>
            <td class="row-actions">
              <button class="btn btn-primary" on:click={() => openEdit(t)}>Edit</button>
              <button class="btn btn-danger" on:click={() => remove(t.id, t.name)}>Revoke</button>
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

  .secret { 
    display: block; 
    margin-top: 8px; 
    word-break: break-all; 
  }

  .row-actions { 
    display: flex; 
    gap: 8px;
  }
</style>
