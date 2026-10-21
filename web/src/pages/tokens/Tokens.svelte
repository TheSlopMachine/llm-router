<script lang="ts">
  import { Button, CodeBlock, FloatingView, HStack, Table, Text, TextField, VStack, Banner, ConfirmAction, Header, Spacer, RelativeTime, type TableColumn, ContentUnavailable, modal } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { formatRelativeTime } from '$lib/time'
  import { createListResource } from '$lib/list-resource.svelte'
  import TokenWizard from './components/TokenWizard.svelte'
  import type { Token, Provider, TokenUsageInfo } from '$lib/types'
  import { n, t, locale } from '$lib/i18n.svelte'

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
      const regenerated: any = await api.tokens.regenerate(regenerateTarget.id)
      newTokenSecret = regenerated?.token ?? regenerated?.Token ?? regenerated?.token_hash ?? null
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

<VStack gap={6}>
  <Header title={t('tokens.list.title')} primary={{ id: 'new', text: t('tokens.actions.new'), icon: 'add', onclick: openCreate }}>
    {#snippet info()}
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
        <HStack halign="end" gap={2}>
          <Button
            buttonStyle="text"
            icon={{ name: 'refresh' }}
            title={t('tokens.actions.regenerate')}
            size="sm"
            ariaLabel={t('tokens.actions.regenerate')}
            onclick={(e) => openRegenerate(tok, e.currentTarget as HTMLElement)}
          />
          <Button
            buttonStyle="text"
            icon={{ name: 'content_copy' }}
            title={t('tokens.actions.clone')}
            size="sm"
            ariaLabel={t('tokens.actions.clone')}
            onclick={(e) => openClone(tok, e.currentTarget as HTMLElement)}
          />
          <Button
            buttonStyle="text"
            icon={{ name: 'edit' }}
            title={t('common.actions.edit')}
            size="sm"
            ariaLabel={t('common.actions.edit')}
            onclick={() => openEdit(tok)}
          />
          <Button
            buttonStyle="text"
            tint="var(--fui-color-danger)"
            size="sm"
            icon={{ name: 'delete' }}
            title={t('tokens.actions.revoke')}
            ariaLabel={t('tokens.actions.revoke')}
            onclick={(e) => openRevoke(tok, e.currentTarget as HTMLElement)}
          />
        </HStack>
      </HStack>
      <Text variant="caption">{fmt(tok.created_at)} · {getLastUsed(tok.id)} · {n(getUsage(tok.id), 'units.api_call.one', 'units.api_call.many')}</Text>
    {/snippet}
    {#snippet cell({ column, row })}
      {@const tok = row as Token}
      {#if column.key === 'name'}
        <Text size="md" weight="medium">{tok.name}</Text>
      {:else if column.key === 'created'}
        <Text size="sm" foregroundStyle="secondary">{fmt(tok.created_at)}</Text>
      {:else if column.key === 'used'}
        {@const lu = resource.data.tokenUsage[tok.id]?.last_used}{#if lu}<RelativeTime date={lu} locale={locale()} />{:else}<Text size="sm">—</Text>{/if}
        <Text size="sm" foregroundStyle="secondary">{n(getUsage(tok.id), 'units.api_call.one', 'units.api_call.many')}</Text>
      {:else if column.key === 'actions'}
        <HStack halign="end" gap={2}>
          <Button
            buttonStyle="text"
            icon={{ name: 'refresh' }}
            title={t('tokens.actions.regenerate')}
            size="sm"
            ariaLabel={t('tokens.actions.regenerate')}
            onclick={(e) => openRegenerate(tok, e.currentTarget as HTMLElement)}
          />
          <Button
            buttonStyle="text"
            icon={{ name: 'content_copy' }}
            title={t('tokens.actions.clone')}
            size="sm"
            ariaLabel={t('tokens.actions.clone')}
            onclick={(e) => openClone(tok, e.currentTarget as HTMLElement)}
          />
          <Button
            buttonStyle="text"
            icon={{ name: 'edit' }}
            title={t('common.actions.edit')}
            size="sm"
            ariaLabel={t('common.actions.edit')}
            onclick={() => openEdit(tok)}
          />
          <Button
            buttonStyle="text"
            tint="var(--fui-color-danger)"
            size="sm"
            icon={{ name: 'delete' }}
            title={t('tokens.actions.revoke')}
            ariaLabel={t('tokens.actions.revoke')}
            onclick={(e) => openRevoke(tok, e.currentTarget as HTMLElement)}
          />
        </HStack>
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
        <TextField bind:value={cloneName} hint={t('tokens.create.name_placeholder')} />
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

  <FloatingView
    open={Boolean(revokeTarget)}
    anchor={revokeAnchor}
    width="sm"
    onclose={() => { revokeTarget = null }}
    label={t('tokens.actions.revoke_title')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('tokens.actions.revoke_title')}
        body={`${t('tokens.actions.revoke_confirm')} "${revokeTarget?.name}"? ${t('common.undo.cannot_undo')}`}
        confirmLabel={t('tokens.actions.revoke')}
        busy={revoking}
        busyLabel={t('tokens.actions.revoking')}
        onCancel={close}
        onConfirm={() => void confirmRevoke(close)} />
    {/snippet}
  </FloatingView>

  <FloatingView
    open={Boolean(regenerateTarget)}
    anchor={regenerateAnchor}
    width="sm"
    onclose={() => { regenerateTarget = null }}
    label={t('tokens.actions.regenerate_title')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('tokens.actions.regenerate_title')}
        body={`${t('tokens.actions.regenerate_for')} "${regenerateTarget?.name}"? ${t('tokens.actions.regenerate_warning')}`}
        confirmLabel={t('tokens.actions.regenerate')}
        busy={regenerating}
        busyLabel={t('tokens.actions.regenerating')}
        onCancel={close}
        onConfirm={() => void confirmRegenerate(close)} />
    {/snippet}
  </FloatingView>
</VStack>
