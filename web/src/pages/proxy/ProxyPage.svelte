<script lang="ts">
  import { VStack, HStack, Text, Button, Table, SectionCard, Spacer, Chip, FloatingView, Banner, ConfirmAction } from '$ui'
  import type { TableColumn } from '$ui'
  import EmptyState from '../../components/EmptyState.svelte'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { openFormModal } from '$lib/modal-helpers'
  import { getErrorMessage } from '$lib/errors'
  import { formatNanoLatency as formatLatency } from '$lib/format'
  import type { Proxy, ProxyStatus, ProxySourceInfo, ProxyPool } from '$lib/types'
  import { t } from '$lib/i18n.svelte'
  import CustomPoolModal from './components/CustomPoolModal.svelte'

  let proxies = $state<Proxy[]>([])
  let sources = $state<ProxySourceInfo[]>([])
  let status = $state<ProxyStatus | null>(null)
  let pools = $state<ProxyPool[]>([])
  let loading = $state(true)
  let requestingRefresh = $state(false)
  let error = $state('')
  let deletePoolTarget = $state<ProxyPool | null>(null)
  let deletePoolAnchor = $state<HTMLElement>()
  let deletingPool = $state(false)

  const proxyColumns: TableColumn[] = [
    { key: 'url', title: t('proxy.url'), width: '1fr', priority: 1 },
    { key: 'latency', title: t('proxy.latency'), width: '90px', align: 'right', priority: 2 },
    { key: 'score', title: t('proxy.score'), width: '80px', align: 'right', priority: 2 },
  ]

  const poolColumns: TableColumn[] = [
    { key: 'name', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'proxies', title: t('proxy.title'), width: '120px', align: 'right', priority: 2 },
    { key: 'actions', title: t('common.labels.actions'), width: 'auto', align: 'right', priority: 1 },
  ]

  const sourceColumns: TableColumn[] = [
    { key: 'name', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'fetched', title: t('proxy.sources.last_fetched'), width: '160px', align: 'right', priority: 2 },
    { key: 'candidates', title: t('proxy.sources.candidates'), width: '110px', align: 'right', priority: 1 },
    { key: 'unsupported', title: t('proxy.sources.unsupported'), width: '110px', align: 'right', priority: 2 },
  ]

  onMount(() => {
    void loadAll()
    const poll = setInterval(() => void loadAll(), 5000)
    return () => clearInterval(poll)
  })

  async function loadAll(): Promise<void> {
    try {
      const [nextProxies, nextSources, nextStatus, nextPools] = await Promise.all([
        api.proxies.list(),
        api.proxies.sources(),
        api.proxies.status(),
        api.proxies.pools.list(),
      ])
      proxies = nextProxies
      sources = nextSources
      status = nextStatus
      pools = nextPools
      error = ''
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function refreshPool(): Promise<void> {
    requestingRefresh = true
    error = ''
    try {
      await api.proxies.refresh()
      await loadAll()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      requestingRefresh = false
    }
  }

  function formatMinutes(nanoseconds: number): string {
    const minutes = Math.round(nanoseconds / 60_000_000_000)
    return `${minutes} ${t('common.time.min')}`
  }

  function formatTime(value?: string): string {
    if (!value || value.startsWith('0001-')) return '—'
    return new Date(value).toLocaleString()
  }

  function openPoolModal(mode: 'create' | 'edit', pool?: ProxyPool): void {
    error = ''
    openFormModal(CustomPoolModal, {
      title: mode === 'edit' ? t('proxy.custom_pools.edit') : t('proxy.custom_pools.new'),
      size: 'medium',
      props: { editingPool: mode === 'edit' ? (pool ?? null) : null },
      onReload: () => void loadAll()
    })
  }

  function openDeletePool(pool: ProxyPool, anchorEl?: HTMLElement): void {
    if (deletePoolTarget?.id === pool.id) {
      deletePoolTarget = null
    } else {
      deletePoolTarget = pool
      deletePoolAnchor = anchorEl
    }
  }

  async function confirmDeletePool(): Promise<void> {
    const pool = deletePoolTarget
    if (!pool || deletingPool) return
    deletingPool = true
    try {
      await api.proxies.pools.remove(pool.id)
      deletePoolTarget = null
      await loadAll()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      deletingPool = false
    }
  }
</script>

<VStack gap={6}>
  <HStack align="center" gap={4}>
    <VStack gap={1} grow>
      <Text tag="h1" size="xl" weight="bold">{t('proxy.title')}</Text>
      <Text tone="soft" size="sm">{t('proxy.pool.auto_refresh')}</Text>
    </VStack>
    <Button
      style="prominent"
      icon={{ name: 'refresh' }}
      disabled={requestingRefresh || status?.refreshing}
      onclick={refreshPool}
    >{status?.refreshing ? t('proxy.pool.refreshing') : t('proxy.pool.refresh')}</Button>
  </HStack>

  {#if error}
    <Banner variant="error" text={error} />
  {/if}

  {#if status?.last_error}
    <Banner variant="error" text={status.last_error} />
  {/if}

  <SectionCard title={t('proxy.pool.status')}>
    {#if status}
      <HStack gap={5} wrap>
        <VStack gap={0}>
          <Text size="xs" tone="soft">{t('proxy.pool.active')}</Text>
          <Text size="base" weight="medium">{status.active} / {status.total}</Text>
        </VStack>
        <VStack gap={0}>
          <Text size="xs" tone="soft">{t('proxy.pool.interval')}</Text>
          <Text size="base" weight="medium">{formatMinutes(status.refresh_interval)}</Text>
        </VStack>
        <VStack gap={0}>
          <Text size="xs" tone="soft">{t('proxy.pool.next_refresh')}</Text>
          <Text size="base" weight="medium">{formatTime(status.next_refresh_at)}</Text>
        </VStack>
        <VStack gap={0}>
          <Text size="xs" tone="soft">{t('proxy.pool.last_refresh')}</Text>
          <Text size="base" weight="medium">{formatTime(status.last_refresh_at)}</Text>
        </VStack>
        {#if status.refreshing}
          <Chip text={t('proxy.pool.refreshing_status')} color="chip-accent" size="small" />
        {/if}
      </HStack>
    {:else}
      <Text tone="soft" size="sm">{t('common.state.loading')}</Text>
    {/if}
  </SectionCard>

  <VStack gap={4}>
    <HStack align="center" gap={4}>
      <VStack gap={1} grow>
        <Text tag="h2" size="md" weight="bold">{t('proxy.custom_pools.title')}</Text>
        <Text tone="soft" size="sm">{t('proxy.custom_pools.desc')}</Text>
      </VStack>
      <Button style="prominent" onclick={() => openPoolModal('create')} icon={{ name: 'add' }}>{t('proxy.custom_pools.new')}</Button>
    </HStack>
    <Table
      columns={poolColumns}
      rows={pools}
      rowKey={(pool) => (pool as ProxyPool).id}
      loading={loading}
    >
      {#snippet cell({ column, row })}
        {@const pool = row as ProxyPool}
        {#if column.key === 'name'}
          <Text size="base" weight="medium">{pool.name}</Text>
        {:else if column.key === 'proxies'}
          <Text size="sm">{pool.entries.length}</Text>
        {:else if column.key === 'actions'}
          <HStack justify="end" gap={2}>
            <Button
              style="text"
              icon={{ name: 'edit' }}
              title={t('common.actions.edit')}
              size="small"
              ariaLabel={t('common.actions.edit')}
              onclick={() => openPoolModal('edit', pool)}
            />
            <Button
              style="text"
              tint="var(--color-danger)"
              size="small"
              icon={{ name: 'delete' }}
              title={t('common.actions.delete')}
              ariaLabel={t('common.actions.delete')}
              onclick={(e) => openDeletePool(pool, e.currentTarget as HTMLElement)}
            />
          </HStack>
        {/if}
      {/snippet}
      {#snippet empty()}
        <EmptyState title={t('proxy.custom_pools.empty')} />
      {/snippet}
    </Table>
  </VStack>

  <VStack gap={4}>
    <VStack gap={1}>
      <Text tag="h2" size="md" weight="bold">{t('proxy.sources.title')}</Text>
      <Text tone="soft" size="sm">{t('proxy.sources.desc')}</Text>
    </VStack>
    <Table
      columns={sourceColumns}
      rows={sources}
      rowKey={(source) => (source as ProxySourceInfo).key}
      loading={loading}
    >
      {#snippet cell({ column, row })}
        {@const source = row as ProxySourceInfo}
        {#if column.key === 'name'}
          <Text size="base" weight="medium">{source.name}</Text>
          <Text size="xs" tone="soft">{source.key}</Text>
          {#if source.last_error}
            <Text size="xs" tone="danger">{source.last_error}</Text>
          {/if}
        {:else if column.key === 'fetched'}
          <Text size="sm">{formatTime(source.last_fetch_at)}</Text>
        {:else if column.key === 'candidates'}
          <Text size="sm">{source.total}</Text>
        {:else if column.key === 'unsupported'}
          <Text size="sm">{source.unsupported}</Text>
        {/if}
      {/snippet}
      {#snippet empty()}
        <EmptyState title={t('proxy.no_sources')} icon="extension" />
      {/snippet}
    </Table>
  </VStack>

  <VStack gap={4}>
    <Text tag="h2" size="md" weight="bold">{t('proxy.pool.healthy')}</Text>
    <Table
      columns={proxyColumns}
      rows={proxies}
      rowKey={(proxy) => (proxy as Proxy).id}
      loading={loading}
    >
      {#snippet cell({ column, row })}
        {@const proxy = row as Proxy}
        {#if column.key === 'url'}
          <HStack gap={2} align="center">
            {#if proxy.location}<Chip text={proxy.location} size="small" />{/if}
            <Text size="base" weight="medium" mono truncate>{proxy.url}</Text>
          </HStack>
        {:else if column.key === 'latency'}
          <Text size="sm">{formatLatency(proxy.latency)}</Text>
        {:else if column.key === 'score'}
          <Text size="sm">{proxy.score.toFixed(2)}</Text>
        {/if}
      {/snippet}
      {#snippet empty()}
        <EmptyState title={t('proxy.pool.empty')} />
      {/snippet}
    </Table>
  </VStack>

  <FloatingView
    open={Boolean(deletePoolTarget)}
    anchor={deletePoolAnchor}
    onclose={() => { deletePoolTarget = null }}
    label={t('proxy.custom_pools.delete')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('proxy.custom_pools.delete')}
        body={`${t('misc.delete_confirm')} "${deletePoolTarget?.name}"? ${t('common.undo.cannot_undo')}`}
        busy={deletingPool}
        busyLabel={t('common.actions.deleting')}
        onCancel={close}
        onConfirm={confirmDeletePool} />
    {/snippet}
  </FloatingView>
</VStack>
