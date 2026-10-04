<script lang="ts">
  import { Button, CodeBlock, FloatingView, HStack, Table, Text, TextEdit, VStack, Banner, ConfirmAction } from '$ui'
  import type { TableColumn } from '$ui'
  import EmptyState from '../../components/EmptyState.svelte'
  import { api } from '$lib/api'
  import { modal } from '$lib/modal.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { formatRelativeTime } from '$lib/time'
  import { createListResource } from '$lib/list-resource.svelte'
  import TokenWizard from './components/TokenWizard.svelte'
  import type { Token, Provider, TokenUsageInfo } from '$lib/types'
  import { n, t } from '$lib/i18n.svelte'

  const tokenColumns: TableColumn[] = [
    { key: 'name', title: t('Name'), width: '1fr', priority: 1 },
    { key: 'created', title: t('Created'), width: '140px', priority: 3 },
    { key: 'used', title: t('Last Used'), width: '1fr', priority: 2 },
    { key: 'actions', title: t('Actions'), width: 'auto', align: 'right', priority: 1 },
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
    const title = mode === 'edit' ? t('Edit token') : t('New token')
    modal.open({
      title,
      content: TokenWizard,
      severity: 'medium',
      size: 'large',
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

  let revokeTarget = $state<{ id: string; name: string } | null>(null)
  let revokeAnchor = $state<HTMLElement>()
  let revoking = $state(false)

  function openRevoke(token: Token, anchorEl?: HTMLElement): void {
    if (revokeTarget?.id === token.id) {
      revokeTarget = null
    } else {
      revokeTarget = { id: token.id, name: token.name }
      revokeAnchor = anchorEl
    }
  }

  let regenerateTarget = $state<{ id: string; name: string } | null>(null)
  let regenerateAnchor = $state<HTMLElement>()
  let regenerating = $state(false)

  function openRegenerate(token: Token, anchorEl?: HTMLElement): void {
    if (regenerateTarget?.id === token.id) {
      regenerateTarget = null
    } else {
      regenerateTarget = { id: token.id, name: token.name }
      regenerateAnchor = anchorEl
    }
  }

  function fmt(d: string): string {
    return new Date(d).toISOString().slice(0, 10)
  }

  let isCloneReady = $derived(!!cloneName.trim() && !!cloneSource && !cloneSaving)

  async function confirmRevoke(close: () => void): Promise<void> {
    if (!revokeTarget || revoking) return
    revoking = true
    try {
      await api.tokens.delete(revokeTarget.id)
      close()
      revokeTarget = null
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    } finally {
      revoking = false
    }
  }

  async function confirmRegenerate(close: () => void): Promise<void> {
    if (!regenerateTarget || regenerating) return
    regenerating = true
    try {
      const res: any = await api.tokens.regenerate(regenerateTarget.id)
      newTokenSecret = res?.token ?? res?.Token ?? res?.token_hash ?? null
      close()
      regenerateTarget = null
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    } finally {
      regenerating = false
    }
  }
  function getUsage(tokenId: string): number {
    return resource.data.tokenUsage[tokenId]?.requests || 0
  }

  function getLastUsed(tokenId: string): string {
    return formatRelativeTime(resource.data.tokenUsage[tokenId]?.last_used, 'long')
  }
</script>

<VStack gap={4}>
  <HStack align="center" gap={4}>
    <VStack gap={1} grow>
      <Text tag="h1" size="lg" weight="bold">{t('Tokens')}</Text>
      <Text tone="soft" size="sm">{t('Router tokens for the')} <code>/v1</code> {t('API. Each token enforces its own model allowlist.')}</Text>
    </VStack>
    <Button style="prominent" onclick={openCreate} icon={{ name: 'add' }}>{t('New Token')}</Button>
  </HStack>

  {#if resource.error}
    <Banner variant="error" text={resource.error} />
  {/if}

  {#if newTokenSecret}
    <VStack gap={2}>
      <Text tone="success" size="sm">{t('Token created. Copy it now — it will not be shown again:')}</Text>
      <CodeBlock text={newTokenSecret} />
    </VStack>
  {/if}

  <Table
    columns={tokenColumns}
    rows={resource.data.tokens}
    rowKey={(tok) => (tok as Token).id}
    loading={resource.loading}
  >
    {#snippet cell({ column, row })}
      {@const tok = row as Token}
      {#if column.key === 'name'}
        <Text size="base" weight="medium">{tok.name}</Text>
      {:else if column.key === 'created'}
        <Text size="sm" tone="soft">{fmt(tok.created_at)}</Text>
      {:else if column.key === 'used'}
        <Text size="sm">{getLastUsed(tok.id)}</Text>
        <Text size="sm" tone="soft">{n(getUsage(tok.id), 'API call', 'API calls', 'вызов API', 'вызова API', 'вызовов API')}</Text>
      {:else if column.key === 'actions'}
        <HStack justify="end" gap={2}>
          <Button
            style="text"
            icon={{ name: 'refresh' }}
            title={t('Regenerate')}
            size="small"
            ariaLabel={t('Regenerate')}
            onclick={(e) => openRegenerate(tok, e.currentTarget as HTMLElement)}
          />
          <Button
            style="text"
            icon={{ name: 'content_copy' }}
            title={t('Clone')}
            size="small"
            ariaLabel={t('Clone')}
            onclick={(e) => openClone(tok, e.currentTarget as HTMLElement)}
          />
          <Button
            style="text"
            icon={{ name: 'edit' }}
            title={t('Edit')}
            size="small"
            ariaLabel={t('Edit')}
            onclick={() => openEdit(tok)}
          />
          <Button
            style="text"
            tint="var(--color-danger)"
            size="small"
            icon={{ name: 'delete' }}
            title={t('Revoke')}
            ariaLabel={t('Revoke')}
            onclick={(e) => openRevoke(tok, e.currentTarget as HTMLElement)}
          />
        </HStack>
      {/if}
    {/snippet}
    {#snippet empty()}
      <EmptyState title={t('No tokens yet')} caption={t('Create a token to call the API')} />
    {/snippet}
  </Table>

  <FloatingView
    open={Boolean(cloneSource)}
    anchor={cloneAnchor}
    onclose={() => { cloneSource = null; cloneError = '' }}
    label={t('Clone token')}
  >
    {#snippet children({ close })}
      <VStack gap={3} style="width: 280px;">
        <Text weight="medium" size="base">{t('Clone token')}</Text>
        {#if cloneError}<Text tone="danger" size="sm">{cloneError}</Text>{/if}
        <TextEdit bind:value={cloneName} hint={t('New token name')} />
        <HStack justify="end" gap={2}>
          <Button size="small" onclick={close} disabled={cloneSaving}>{t('Cancel')}</Button>
          <Button
            size="small"
            style="prominent"
            disabled={!isCloneReady}
            onclick={async () => {
              if (!isCloneReady || !cloneSource) return
              const name = cloneName.trim()
              cloneSaving = true
              try {
                const res: any = await api.tokens.create({ name, rules: cloneSource.rules } as any)
                newTokenSecret = res?.token ?? null
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
            {cloneSaving ? t('Creating…') : t('Create')}
          </Button>
        </HStack>
      </VStack>
    {/snippet}
  </FloatingView>

  <FloatingView
    open={Boolean(revokeTarget)}
    anchor={revokeAnchor}
    onclose={() => { revokeTarget = null }}
    label={t('Revoke token')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('Revoke token')}
        body={`${t('Are you sure you want to revoke token')} "${revokeTarget?.name}"? ${t('This action cannot be undone.')}`}
        confirmLabel={t('Revoke')}
        busy={revoking}
        busyLabel={t('Revoking…')}
        onCancel={close}
        onConfirm={() => void confirmRevoke(close)} />
    {/snippet}
  </FloatingView>

  <FloatingView
    open={Boolean(regenerateTarget)}
    anchor={regenerateAnchor}
    onclose={() => { regenerateTarget = null }}
    label={t('Regenerate token')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('Regenerate token')}
        body={`${t('Regenerate secret for')} "${regenerateTarget?.name}"? ${t('The old secret will be invalidated immediately.')}`}
        confirmLabel={t('Regenerate')}
        busy={regenerating}
        busyLabel={t('Regenerating…')}
        onCancel={close}
        onConfirm={() => void confirmRegenerate(close)} />
    {/snippet}
  </FloatingView>
</VStack>
