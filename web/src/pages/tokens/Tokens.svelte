<script lang="ts">
  import { Button, CodeBlock, HStack, Table, Text, TextEdit, VStack } from '$ui'
  import type { TableColumn } from '$ui'
  import { api } from '$lib/api'
  import { modal } from '$lib/modal.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { formatRelativeTime } from '$lib/time'
  import { createListResource } from '$lib/list-resource.svelte'
  import TokenWizard from './components/TokenWizard.svelte'
  import type { Token, Provider, TokenUsageInfo } from '$lib/types'
  import { n, t } from '$lib/i18n.svelte'

  const tokenColumns: TableColumn[] = [
    { key: 'name', title: t('Name'), width: '1fr' },
    { key: 'created', title: t('Created'), width: '140px' },
    { key: 'used', title: t('Last Used'), width: '1fr' },
    { key: 'actions', title: t('Actions'), width: 'auto', align: 'right' },
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
  let cloneName = $state('')
  let cloneSaving = $state(false)
  let cloneError = $state('')

  function openClone(token: Token): void {
    resource.error = ''
    cloneSource = token
    cloneName = `${token.name} (copy)`
    cloneError = ''
    modal.open({
      title: t('Clone token'),
      contentSnippet: cloneDialog,
      severity: 'medium',
      size: 'small'
    })
  }

  async function createClone(): Promise<void> {
    const name = cloneName.trim()
    if (!name || !cloneSource || cloneSaving) return
    cloneSaving = true
    try {
      await api.tokens.create({ name, rules: cloneSource.rules } as any)
      await resource.reload()
      cloneSource = null
      modal.close()
    } catch (e) {
      cloneError = getErrorMessage(e)
    } finally {
      cloneSaving = false
    }
  }

  async function regenerate(id: string, name: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('Regenerate token'),
      message: `${t('Regenerate secret for')} "${name}"? ${t('The old secret will be invalidated immediately.')}`,
      severity: 'high',
      confirmText: t('Regenerate'),
      confirmRole: 'destructive'
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
      confirmRole: 'destructive'
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
  function getUsage(tokenId: string): number {
    return resource.data.tokenUsage[tokenId]?.requests || 0
  }

  function getLastUsed(tokenId: string): string {
    return formatRelativeTime(resource.data.tokenUsage[tokenId]?.last_used, 'long')
  }
</script>

{#snippet cloneDialog()}
  <VStack gap={4}>
    {#if cloneError}<Text tone="danger" size="sm">{cloneError}</Text>{/if}
    <TextEdit bind:value={cloneName} hint={t('New token name')} />
    <HStack justify="end" gap={2}>
      <Button onclick={() => modal.close()} disabled={cloneSaving}>{t('Cancel')}</Button>
      <Button
        style="prominent"
        onclick={() => void createClone()}
        disabled={!cloneName.trim() || cloneSaving}
      >{cloneSaving ? t('Creating…') : t('Create')}</Button>
    </HStack>
  </VStack>
{/snippet}

<VStack gap={4}>
  <HStack align="center" gap={4}>
    <VStack gap={1} grow>
      <Text tag="h1" size="lg" weight="bold">{t('Tokens')}</Text>
      <Text tone="soft" size="sm">{t('Router tokens for the')} <code>/v1</code> {t('API. Each token enforces its own model allowlist.')}</Text>
    </VStack>
    <Button style="prominent" onclick={openCreate} icon={{ name: 'add' }}>{t('New Token')}</Button>
  </HStack>

  {#if resource.error}
    <Text tone="danger" size="sm">{resource.error}</Text>
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
            onclick={() => regenerate(tok.id, tok.name)}
          />
          <Button
            style="text"
            icon={{ name: 'content_copy' }}
            title={t('Clone')}
            size="small"
            ariaLabel={t('Clone')}
            onclick={() => openClone(tok)}
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
            tint="#ff0000ff"
            size="small"
            icon={{ name: 'delete' }}
            title={t('Revoke')}
            ariaLabel={t('Revoke')}
            onclick={() => remove(tok.id, tok.name)}
          />
        </HStack>
      {/if}
    {/snippet}
    {#snippet empty()}
      <VStack align="center" gap={2}>
        <Text size="sm" tone="soft">{t('No tokens yet')}</Text>
        <Text size="sm" tone="soft">{t('Create a token to access the /v1 API with model-specific permissions.')}</Text>
        <Button style="prominent" icon={{ name: 'add' }} onclick={openCreate}>{t('Create Your First Token')}</Button>
      </VStack>
    {/snippet}
  </Table>
</VStack>
