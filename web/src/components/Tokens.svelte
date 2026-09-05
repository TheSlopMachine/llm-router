<script lang="ts">
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import { createListResource } from '../lib/list-resource.svelte'
  import TokenWizard from './wizards/TokenWizard.svelte'
  import EmptyState from './EmptyState.svelte'
  import ActionDropdown from './ActionDropdown.svelte'
  import type { Token, Provider, TokenUsageInfo } from '../lib/types'

  const tokenActions = [
    { id: 'edit', label: 'Edit', icon: 'edit' },
    { id: 'clone', label: 'Clone', icon: 'content_copy' },
    { id: 'regenerate', label: 'Regenerate', icon: 'refresh' }
  ]

  function handleTokenAction(id: string, t: Token): void {
    switch (id) {
      case 'edit':
        openEdit(t)
        break
      case 'clone':
        openClone(t)
        break
      case 'regenerate':
        regenerate(t.id, t.name)
        break
    }
  }

  const resource = createListResource<{ tokens: Token[]; providers: Provider[]; tokenUsage: Record<string, TokenUsageInfo> }>(
    async () => {
      const [t, p, u] = await Promise.all([api.tokens.list(), api.providers.list(), api.tokens.usage()])
      return { tokens: t || [], providers: p || [], tokenUsage: u || {} }
    },
    { tokens: [], providers: [], tokenUsage: {} }
  )
  let newTokenSecret = $state<string | null>(null)

  function openCreate(): void {
    resource.error = ''

    modal.open({
      title: 'New token',
      content: TokenWizard,
      severity: 'medium',
      size: 'large',
      props: {
        providers: resource.data.providers,
        editingToken: null,
        cloningToken: null,
        onComplete: async (_result: { token?: string }) => {
          // Token is shown inside the wizard's completion screen — no outside banner.
          await resource.reload()
        }
      }
    })
  }

  async function openEdit(token: Token): Promise<void> {
    resource.error = ''

    modal.open({
      title: 'Edit token',
      content: TokenWizard,
      severity: 'medium',
      size: 'large',
      props: {
        providers: resource.data.providers,
        editingToken: token,
        cloningToken: null,
        onComplete: async () => {
          await resource.reload()
        }
      }
    })
  }

  function openClone(token: Token): void {
    resource.error = ''
    modal.open({
      title: 'Clone token',
      content: TokenWizard,
      severity: 'medium',
      size: 'large',
      props: {
        providers: resource.data.providers,
        editingToken: null,
        cloningToken: token,
        onComplete: async (_result: { token?: string }) => {
          await resource.reload()
        }
      }
    })
  }

  async function regenerate(id: string, name: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Regenerate token',
      message: `Regenerate secret for "${name}"? The old secret will be invalidated immediately.`,
      severity: 'high',
      confirmText: 'Regenerate',
      cancelText: 'Cancel',
      danger: true
    })
    if (!confirmed) return
    try {
      const res: any = await api.tokens.regenerate(id)
      newTokenSecret = res?.token ?? res?.Token ?? res?.token_hash ?? null
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
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
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }

  function fmt(d: string): string {
    return new Date(d).toISOString().slice(0, 10)
  }
  function shortId(id: string): string {
    return id.slice(0, 12) + '…'
  }
  function getUsage(tokenId: string): number {
    return resource.data.tokenUsage[tokenId]?.requests || 0
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
    return formatRelativeTime(resource.data.tokenUsage[tokenId]?.last_used)
  }
</script>

<div class="page-header">
  <div>
    <h1>Tokens</h1>
    <p>Router tokens for the <code>/v1</code> API. Each token enforces its own model allowlist.</p>
  </div>
  {#if resource.data.tokens.length > 0}
    <button class="btn btn-primary" onclick={openCreate}>
      <span class="icon">add</span>
      New Token
    </button>
  {/if}
</div>

{#if resource.error}
  <div class="error-msg">{resource.error}</div>
{/if}

{#if newTokenSecret}
  <div class="success-msg">
    Token created. Copy it now — it will not be shown again:<br />
    <span class="mono secret">{newTokenSecret}</span>
  </div>
{/if}

{#if resource.loading}
  <div class="loading">Loading tokens...</div>
{:else if resource.data.tokens.length === 0}
  <EmptyState
    icon="key"
    message="No tokens yet"
    hint="Create a token to access the /v1 API with model-specific permissions."
    buttonText="Create Your First Token"
    buttonIcon="add"
    onButtonClick={openCreate}
  />
{:else}
  <div class="card tokens-card">
    <div class="card-header"><h2>Tokens</h2></div>
    <table>
      <thead>
        <tr><th>Name</th><th>ID</th><th>Created</th><th>Last Used</th><th>API calls</th><th></th></tr>
      </thead>
      <tbody>
        {#each resource.data.tokens as t}
          <tr>
            <td>{t.name}</td>
            <td class="mono">{shortId(t.id)}</td>
            <td>{fmt(t.created_at)}</td>
            <td>{getLastUsed(t.id)}</td>
            <td>{getUsage(t.id).toLocaleString()}</td>
            <td class="row-actions">
              <ActionDropdown actions={tokenActions} label="Actions" rounded="lg" onaction={(id) => handleTokenAction(id, t)} />
              <button class="btn btn-danger" onclick={() => remove(t.id, t.name)}>Revoke</button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .secret {
    display: block;
    margin-top: 8px;
    word-break: break-all;
  }

  .tokens-card {
    overflow: visible;
  }

  .row-actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  :global(.tokens-card .dropdown-trigger) {
    height: 32px;
  }
</style>
