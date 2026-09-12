<script lang="ts">
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import { formatRelativeTime } from '../lib/time'
  import { createListResource } from '../lib/list-resource.svelte'
  import TokenWizard from './wizards/TokenWizard.svelte'
  import EmptyState from './EmptyState.svelte'
  import ActionDropdown from './ActionDropdown.svelte'
  import type { Token, Provider, TokenUsageInfo } from '../lib/types'
  import { t } from '../lib/i18n.svelte'

  let tokenActions = $derived([
    { id: 'edit', label: t('Edit'), icon: 'edit' },
    { id: 'clone', label: t('Clone'), icon: 'content_copy' },
    { id: 'regenerate', label: t('Regenerate'), icon: 'refresh' }
  ])

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

  function openWizard(mode: 'create' | 'edit' | 'clone', token?: Token): void {
    resource.error = ''
    const title = mode === 'edit' ? t('Edit token') : mode === 'clone' ? t('Clone token') : t('New token')
    modal.open({
      title,
      content: TokenWizard,
      severity: 'medium',
      size: 'large',
      props: {
        providers: resource.data.providers,
        editingToken: mode === 'edit' ? (token ?? null) : null,
        cloningToken: mode === 'clone' ? (token ?? null) : null,
        onComplete: async () => {
          await resource.reload()
        }
      }
    })
  }

  function openCreate(): void { openWizard('create') }
  function openEdit(token: Token): void { openWizard('edit', token) }
  function openClone(token: Token): void { openWizard('clone', token) }

  async function regenerate(id: string, name: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('Regenerate token'),
      message: `${t('Regenerate secret for')} "${name}"? ${t('The old secret will be invalidated immediately.')}`,
      severity: 'high',
      confirmText: t('Regenerate'),
      cancelText: t('Cancel'),
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
      title: t('Revoke token'),
      message: `${t('Are you sure you want to revoke token')} "${name}"? ${t('This action cannot be undone.')}`,
      severity: 'medium',
      confirmText: t('Revoke'),
      cancelText: t('Cancel'),
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

  function getLastUsed(tokenId: string): string {
    return formatRelativeTime(resource.data.tokenUsage[tokenId]?.last_used, 'long')
  }
</script>

<div class="page-header">
  <div>
    <h1>{t('Tokens')}</h1>
    <p>{t('Router tokens for the')} <code>/v1</code> {t('API. Each token enforces its own model allowlist.')}</p>
  </div>
  {#if resource.data.tokens.length > 0}
    <button class="btn btn-primary" onclick={openCreate}>
      <span class="icon">add</span>
      {t('New Token')}
    </button>
  {/if}
</div>

{#if resource.error}
  <div class="error-msg">{resource.error}</div>
{/if}

{#if newTokenSecret}
  <div class="success-msg">
    {t('Token created. Copy it now — it will not be shown again:')}<br />
    <span class="mono secret">{newTokenSecret}</span>
  </div>
{/if}

{#if resource.loading}
  <div class="loading">{t('Loading tokens...')}</div>
{:else if resource.data.tokens.length === 0}
  <EmptyState
    icon="key"
    message={t('No tokens yet')}
    hint={t('Create a token to access the /v1 API with model-specific permissions.')}
    buttonText={t('Create Your First Token')}
    buttonIcon="add"
    onButtonClick={openCreate}
  />
{:else}
  <div class="card tokens-card">
    <div class="card-header"><h2>{t('Tokens')}</h2></div>
    <table>
      <thead>
        <tr><th>{t('Name')}</th><th>{t('ID')}</th><th>{t('Created')}</th><th>{t('Last Used')}</th><th>{t('API calls')}</th><th></th></tr>
      </thead>
      <tbody>
        {#each resource.data.tokens as tok}
          <tr>
            <td>{tok.name}</td>
            <td class="mono">{shortId(tok.id)}</td>
            <td>{fmt(tok.created_at)}</td>
            <td>{getLastUsed(tok.id)}</td>
            <td>{getUsage(tok.id).toLocaleString()}</td>
            <td class="row-actions">
              <ActionDropdown actions={tokenActions} label={t('Actions')} rounded="lg" onaction={(id) => handleTokenAction(id, tok)} />
              <button class="btn btn-danger" onclick={() => remove(tok.id, tok.name)}>{t('Revoke')}</button>
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
