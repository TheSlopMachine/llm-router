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
    { key: 'url', title: t('URL'), width: '1fr', priority: 1 },
    { key: 'latency', title: t('Latency'), width: '90px', align: 'right', priority: 2 },
    { key: 'score', title: t('Score'), width: '80px', align: 'right', priority: 2 },
  ]

  const poolColumns: TableColumn[] = [
    { key: 'name', title: t('Name'), width: '1fr', priority: 1 },
    { key: 'proxies', title: t('Proxies'), width: '120px', align: 'right', priority: 2 },
    { key: 'actions', title: t('Actions'), width: 'auto', align: 'right', priority: 1 },
  ]

  const sourceColumns: TableColumn[] = [
    { key: 'name', title: t('Name'), width: '1fr', priority: 1 },
    { key: 'fetched', title: t('Last fetched'), width: '160px', align: 'right', priority: 2 },
    { key: 'candidates', title: t('Candidates'), width: '110px', align: 'right', priority: 1 },
    { key: 'unsupported', title: t('Unsupported'), width: '110px', align: 'right', priority: 2 },
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
    return `${minutes} ${t('min')}`
  }

  function formatTime(value?: string): string {
    if (!value || value.startsWith('0001-')) return '—'
    return new Date(value).toLocaleString()
  }

  function openPoolModal(mode: 'create' | 'edit', pool?: ProxyPool): void {
    error = ''
    openFormModal(CustomPoolModal, {
      title: mode === 'edit' ? t('Edit pool') : t('New pool'),
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
      <Text tag="h1" size="xl" weight="bold">{t('Proxies')}</Text>
      <Text tone="soft" size="sm">{t('The pool refreshes automatically and keeps unhealthy proxies for later checks.')}</Text>
    </VStack>
    <Button
      style="prominent"
      icon={{ name: 'refresh' }}
      disabled={requestingRefresh || status?.refreshing}
      onclick={refreshPool}
    >{status?.refreshing ? t('Refreshing…') : t('Refresh pool')}</Button>
  </HStack>

  {#if error}
    <Banner variant="error" text={error} />
  {/if}

  {#if status?.last_error}
    <Banner variant="error" text={status.last_error} />
  {/if}

  <SectionCard title={t('Pool status')}>
    {#if status}
      <HStack gap={5} wrap>
        <VStack gap={0}>
          <Text size="xs" tone="soft">{t('Active proxies')}</Text>
          <Text size="base" weight="medium">{status.active} / {status.total}</Text>
        </VStack>
        <VStack gap={0}>
          <Text size="xs" tone="soft">{t('Refresh interval')}</Text>
          <Text size="base" weight="medium">{formatMinutes(status.refresh_interval)}</Text>
        </VStack>
        <VStack gap={0}>
          <Text size="xs" tone="soft">{t('Next refresh')}</Text>
          <Text size="base" weight="medium">{formatTime(status.next_refresh_at)}</Text>
        </VStack>
        <VStack gap={0}>
          <Text size="xs" tone="soft">{t('Last refresh')}</Text>
          <Text size="base" weight="medium">{formatTime(status.last_refresh_at)}</Text>
        </VStack>
        {#if status.refreshing}
          <Chip text={t('Refreshing')} color="chip-accent" size="small" />
        {/if}
      </HStack>
    {:else}
      <Text tone="soft" size="sm">{t('Loading…')}</Text>
    {/if}
  </SectionCard>

  <VStack gap={4}>
    <HStack align="center" gap={4}>
      <VStack gap={1} grow>
        <Text tag="h2" size="md" weight="bold">{t('Custom pools')}</Text>
        <Text tone="soft" size="sm">{t('Manually managed pools.')}</Text>
      </VStack>
      <Button style="prominent" onclick={() => openPoolModal('create')} icon={{ name: 'add' }}>{t('New pool')}</Button>
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
              title={t('Edit')}
              size="small"
              ariaLabel={t('Edit')}
              onclick={() => openPoolModal('edit', pool)}
            />
            <Button
              style="text"
              tint="var(--color-danger)"
              size="small"
              icon={{ name: 'delete' }}
              title={t('Delete')}
              ariaLabel={t('Delete')}
              onclick={(e) => openDeletePool(pool, e.currentTarget as HTMLElement)}
            />
          </HStack>
        {/if}
      {/snippet}
      {#snippet empty()}
        <EmptyState title={t('No custom pools yet.')} />
      {/snippet}
    </Table>
  </VStack>

  <VStack gap={4}>
    <VStack gap={1}>
      <Text tag="h2" size="md" weight="bold">{t('Proxy sources')}</Text>
      <Text tone="soft" size="sm">{t('Plugins provide candidate URLs. The library retains and checks them.')}</Text>
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
        <EmptyState title={t('No proxy list sources installed. Install a proxy-source plugin (e.g. proxifly).')} icon="extension" />
      {/snippet}
    </Table>
  </VStack>

  <VStack gap={4}>
    <Text tag="h2" size="md" weight="bold">{t('Healthy proxies')}</Text>
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
        <EmptyState title={t('No healthy proxies in the pool.')} />
      {/snippet}
    </Table>
  </VStack>

  <FloatingView
    open={Boolean(deletePoolTarget)}
    anchor={deletePoolAnchor}
    onclose={() => { deletePoolTarget = null }}
    label={t('Delete pool')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('Delete pool')}
        body={`${t('Are you sure you want to delete')} "${deletePoolTarget?.name}"? ${t('This action cannot be undone.')}`}
        busy={deletingPool}
        busyLabel={t('Deleting…')}
        onCancel={close}
        onConfirm={confirmDeletePool} />
    {/snippet}
  </FloatingView>
</VStack>
