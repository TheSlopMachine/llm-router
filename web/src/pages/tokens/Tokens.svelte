<script lang="ts">
  import { Button, CodeBlock, DeleteButton, FloatingView, HStack, Table, Text, TextField, VStack, Banner, Header, Spacer, RelativeTime, confirmAt, type TableColumn, ContentUnavailable, modal } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { createListResource } from '$lib/list-resource.svelte'
  import TokenWizard from './components/TokenWizard.svelte'
  import type { Token, Provider, TokenUsageInfo } from '$lib/types'
  import { n, t } from '$lib/i18n.svelte'

  const tokenColumns: TableColumn[] = [
    { key: 'name', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'created', title: t('common.actions.created'), width: 'var(--fui-table-col-xl)', priority: 3 },
    { key: 'used', title: t('credentials.last_used'), width: '1fr', priority: 2 },
    { key: 'actions', title: t('common.labels.actions'), width: 'auto', align: 'right', priority: 1 },
  ]

  const resource = createListResource<{ tokens: Token[]; providers: Provider[]; tokenUsage: Record<string, TokenUsageInfo> }>(
    async () => {
      const [t, p, u] = await Promise.all([api.tokens.list(), api.providers.list(), api.tokens.usage()])
      return { tokens: t || [], providers: p || [], tokenUsage: u || {} }
    },
    { tokens: [], providers: [], tokenUsage: {} }
  )
  let newTokenSecret = $state<string | null>(null)

  function openWizard(mode: 'create' | 'edit', token?: Token): void {
    resource.error = ''
    const title = mode === 'edit' ? t('tokens.edit.title') : t('tokens.actions.new')
    modal.open({
      title,
      content: TokenWizard,
      severity: 'medium',
      size: 'lg',
      props: {
        providers: resource.data.providers,
        editingToken: mode === 'edit' ? (token ?? null) : null,
        onComplete: async () => {
          await resource.reload()
        }
      }
    })
  }

  function openCreate(): void { openWizard('create') }
  function openEdit(token: Token): void { openWizard('edit', token) }

  let cloneSource = $state<Token | null>(null)
  let cloneAnchor = $state<HTMLElement>()
  let cloneName = $state('')
  let cloneSaving = $state(false)
  let cloneError = $state('')

  function openClone(token: Token, anchorEl?: HTMLElement): void {
    if (cloneSource?.id === token.id) {
      cloneSource = null
    } else {
      resource.error = ''
      cloneSource = token
      cloneName = `${token.name} (copy)`
      cloneError = ''
      cloneAnchor = anchorEl
    }
  }

  async function revoke(token: Token, anchor: HTMLElement): Promise<void> {
    resource.error = ''
    try {
      const revoked = await confirmAt(anchor, {
        title: t('tokens.actions.revoke_title'),
        body: `${t('tokens.actions.revoke_confirm')} "${token.name}"? ${t('common.undo.cannot_undo')}`,
        confirmLabel: t('tokens.actions.revoke'),
        busyLabel: t('tokens.actions.revoking'),
        run: () => api.tokens.delete(token.id),
      })
      if (revoked) await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }

  async function regenerate(token: Token, anchor: HTMLElement): Promise<void> {
    resource.error = ''
    try {
      const regenerated = await confirmAt(anchor, {
        title: t('tokens.actions.regenerate_title'),
        body: `${t('tokens.actions.regenerate_for')} "${token.name}"? ${t('tokens.actions.regenerate_warning')}`,
        confirmLabel: t('tokens.actions.regenerate'),
        busyLabel: t('tokens.actions.regenerating'),
        run: async () => {
          const result: any = await api.tokens.regenerate(token.id)
          newTokenSecret = result?.token ?? result?.Token ?? result?.token_hash ?? null
        },
      })
      if (regenerated) await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }

  function fmt(d: string): string {
    return new Date(d).toISOString().slice(0, 10)
  }

  let isCloneReady = $derived(!!cloneName.trim() && !!cloneSource && !cloneSaving)

  function getUsage(tokenId: string): number {
    return resource.data.tokenUsage[tokenId]?.requests || 0
  }

</script>

{#snippet lastUsed(tok: Token)}
  {@const at = resource.data.tokenUsage[tok.id]?.last_used}
  {#if at}<RelativeTime date={at} />{:else}—{/if}
{/snippet}

{#snippet tokenActions(tok: Token)}
  <HStack halign="end" gap={2}>
    <Button buttonStyle="text" size="sm" icon={{ name: 'refresh' }} title={t('tokens.actions.regenerate')} ariaLabel={t('tokens.actions.regenerate')}
      onclick={(e) => void regenerate(tok, e.currentTarget as HTMLElement)} />
    <Button buttonStyle="text" size="sm" icon={{ name: 'content_copy' }} title={t('tokens.actions.clone')} ariaLabel={t('tokens.actions.clone')}
      onclick={(e) => openClone(tok, e.currentTarget as HTMLElement)} />
    <Button buttonStyle="text" size="sm" icon={{ name: 'edit' }} title={t('common.actions.edit')} ariaLabel={t('common.actions.edit')}
      onclick={() => openEdit(tok)} />
    <DeleteButton size="sm" label={t('tokens.actions.revoke')} onclick={(e) => void revoke(tok, e.currentTarget as HTMLElement)} />
  </HStack>
{/snippet}

<VStack gap={6}>
  <Header title={t('tokens.list.title')} primary={{ id: 'new', text: t('tokens.actions.new'), icon: 'add', onclick: openCreate }}>
    {#snippet subtitle()}
      {t('tokens.list.router_for')} <code>/v1</code> {t('tokens.list.subtitle')}
    {/snippet}
  </Header>

  {#if resource.error}
    <Banner tint="danger" text={resource.error} />
  {/if}

  {#if newTokenSecret}
    <VStack gap={2}>
      <Text tint="success" size="sm">{t('tokens.success.copy_warning')}</Text>
      <CodeBlock text={newTokenSecret} />
    </VStack>
  {/if}

  <Table
    columns={tokenColumns}
    rows={resource.data.tokens}
    rowKey={(tok) => (tok as Token).id}
    loading={resource.loading}
  >
    {#snippet card({ row })}
      {@const tok = row as Token}
      <HStack gap={3} valign="center">
        <Text variant="value">{tok.name}</Text>
        <Spacer />
        {@render tokenActions(tok)}
      </HStack>
      <Text variant="caption">{fmt(tok.created_at)} · {@render lastUsed(tok)} · {n(getUsage(tok.id), 'units.api_call.one', 'units.api_call.many')}</Text>
    {/snippet}
    {#snippet cell({ column, row })}
      {@const tok = row as Token}
      {#if column.key === 'name'}
        <Text size="md" weight="medium">{tok.name}</Text>
      {:else if column.key === 'created'}
        <Text size="sm" foregroundStyle="secondary">{fmt(tok.created_at)}</Text>
      {:else if column.key === 'used'}
        <Text size="sm">{@render lastUsed(tok)}</Text>
        <Text size="sm" foregroundStyle="secondary">{n(getUsage(tok.id), 'units.api_call.one', 'units.api_call.many')}</Text>
      {:else if column.key === 'actions'}
        {@render tokenActions(tok)}
      {/if}
    {/snippet}
    {#snippet empty()}
      <ContentUnavailable title={t('tokens.list.empty')} caption={t('tokens.create.api_desc')} />
    {/snippet}
  </Table>

  <FloatingView
    open={Boolean(cloneSource)}
    anchor={cloneAnchor}
    width="sm"
    onclose={() => { cloneSource = null; cloneError = '' }}
    label={t('tokens.actions.clone_title')}
  >
    {#snippet children({ close })}
      <VStack gap={3}>
        <Text variant="value">{t('tokens.actions.clone_title')}</Text>
        {#if cloneError}<Banner tint="danger" text={cloneError} />{/if}
        <TextField bind:value={cloneName} placeholder={t('tokens.create.name_placeholder')} />
        <HStack halign="end" gap={2}>
          <Button size="sm" buttonStyle="text" onclick={close} disabled={cloneSaving}>{t('common.actions.cancel')}</Button>
          <Button
            size="sm"
            buttonStyle="prominent"
            disabled={!isCloneReady}
            onclick={async () => {
              if (!isCloneReady || !cloneSource) return
              const name = cloneName.trim()
              cloneSaving = true
              try {
                const created: any = await api.tokens.create({ name, rules: cloneSource.rules } as any)
                newTokenSecret = created?.token ?? null
                close()
                cloneSource = null
                await resource.reload()
              } catch (e) {
                cloneError = getErrorMessage(e)
              } finally {
                cloneSaving = false
              }
            }}
          >
            {cloneSaving ? t('common.actions.creating') : t('common.actions.create')}
          </Button>
        </HStack>
      </VStack>
    {/snippet}
  </FloatingView>
</VStack>
