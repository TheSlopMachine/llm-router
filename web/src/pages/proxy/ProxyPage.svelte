<script lang="ts">
  import { VStack, HStack, Text, Button, Table, SectionCard, Chip, FloatingView, Banner, ConfirmAction, Select, Header, Stat, Grid, Picker, Spacer, ToolbarItem, Accordion, StackedBar, Progress, RelativeTime, Field } from '$ui'
  import type { TableColumn } from '$ui'
  import EmptyState from '../../FUI/composite/EmptyState.svelte'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { openFormModal } from '$lib/modal-helpers'
  import { getErrorMessage } from '$lib/errors'
  import { formatNanoLatency as formatLatency } from '$lib/format'
  import type { Proxy, ProxyStatus, ProxySourceInfo, ProxyPool } from '$lib/types'
  import { t, locale } from '$lib/i18n.svelte'
  import CustomPoolModal from './components/CustomPoolModal.svelte'

  export type ProxyTab = 'status' | 'sources' | 'pools'

  let { tab = 'status', ontabchange } = $props<{
    tab?: ProxyTab
    ontabchange?: (next: ProxyTab) => void
  }>()

  let proxies = $state<Proxy[]>([])
  let sources = $state<ProxySourceInfo[]>([])
  let status = $state<ProxyStatus | null>(null)
  let pools = $state<ProxyPool[]>([])
  let loading = $state(true)
  let requestingRefresh = $state(false)
  let recheckingBanned = $state(false)
  let recheckReason = $state('')
  let recheckSource = $state('')
  let recheckQueued = $state<number | null>(null)
  let error = $state('')
  let deletePoolTarget = $state<ProxyPool | null>(null)
  let deletePoolAnchor = $state<HTMLElement>()
  let deletingPool = $state(false)

  const proxyColumns: TableColumn[] = [
    { key: 'url', title: t('proxy.url'), width: '1fr', priority: 1 },
    { key: 'latency', title: t('proxy.latency'), width: 'var(--fui-table-col-sm)', align: 'right', priority: 2 },
    { key: 'score', title: t('proxy.score'), width: 'var(--fui-table-col-xs)', align: 'right', priority: 2 },
  ]

  const poolColumns: TableColumn[] = [
    { key: 'name', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'proxies', title: t('proxy.title'), width: 'var(--fui-table-col-lg)', align: 'right', priority: 2 },
    { key: 'actions', title: t('common.labels.actions'), width: 'auto', align: 'right', priority: 1 },
  ]

  // One table for sources: the configuration row (name, key, last fetch, candidates) joined with
  // the live pool counters by source name.
  const sourceColumns: TableColumn[] = [
    { key: 'name', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'fetched', title: t('proxy.sources.last_fetched'), width: 'max-content', align: 'right', priority: 3 },
    { key: 'candidates', title: t('proxy.sources.candidates'), width: 'max-content', align: 'right', priority: 2 },
    { key: 'alive', title: t('proxy.pool.alive'), width: 'max-content', align: 'right', priority: 1 },
    { key: 'suspect', title: t('proxy.pool.suspect'), width: 'max-content', align: 'right', priority: 2 },
    { key: 'banned', title: t('proxy.pool.banned'), width: 'max-content', align: 'right', priority: 1 },
  ]

  type LiveSource = ProxyStatus['sources'][number]
  interface MergedSource { name: string; cfg: ProxySourceInfo | null; live: LiveSource | null }
  const mergedSources = $derived.by<MergedSource[]>(() => {
    const live = status?.sources ?? []
    const rows: MergedSource[] = sources.map((cfg) => ({
      name: cfg.name,
      cfg,
      live: live.find((l) => l.source === cfg.name) ?? null,
    }))
    for (const l of live) if (!rows.some((r) => r.name === l.source)) rows.push({ name: l.source, cfg: null, live: l })
    return rows
  })

  let openSections = $state<string[]>([])
  const loadLabel = $derived(status ? `${status.inflight} / ${status.limit}` : '')

  const laneColumns: TableColumn[] = [
    { key: 'lane', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'inflight', title: t('proxy.pool.inflight'), width: 'var(--fui-table-col-md)', align: 'right', priority: 1 },
    { key: 'queued', title: t('proxy.pool.queued'), width: 'var(--fui-table-col-md)', align: 'right', priority: 1 },
  ]

  const recheckReasonOptions = [
    { value: '', label: t('proxy.recheck_banned.any_reason') },
    { value: 'timeout', label: 'timeout' },
    { value: 'refused', label: 'refused' },
    { value: 'rejected', label: 'rejected' },
    { value: 'eof_reset', label: 'eof_reset' },
    { value: 'dns', label: 'dns' },
    { value: 'tls', label: 'tls' },
    { value: 'bad_status', label: 'bad_status' },
    { value: 'local_net', label: 'local_net' },
    { value: 'other', label: 'other' },
  ]

  const recheckSourceOptions = $derived(
    status
      ? [{ value: '', label: t('proxy.recheck_banned.any_source') }, ...status.sources.map((source) => ({ value: source.source, label: source.source }))]
      : [{ value: '', label: t('proxy.recheck_banned.any_source') }]
  )

  onMount(() => {
    void loadAll()
    const poll = setInterval(() => void loadStatus(), 2000)
    return () => clearInterval(poll)
  })

  async function loadStatus(): Promise<void> {
    try {
      status = await api.proxies.status()
      error = ''
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

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
      await loadStatus()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      requestingRefresh = false
    }
  }

  async function recheckBanned(): Promise<void> {
    recheckingBanned = true
    recheckQueued = null
    error = ''
    try {
      const result = await api.proxies.recheckBanned({
        reason: recheckReason,
        source: recheckSource,
      })
      recheckQueued = result.queued
      await loadStatus()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      recheckingBanned = false
    }
  }

  function formatBanReasons(reasons: Record<string, number>): string {
    return Object.entries(reasons)
      .filter(([, count]) => count > 0)
      .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
      .map(([reason, count]) => `${reason}=${count}`)
      .join(' ') || '-'
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

  const hasBanReasons = $derived(status && Object.keys(status.ban_reasons ?? {}).some((k) => (status?.ban_reasons[k] ?? 0) > 0))
</script>

{#snippet healthyBody()}
  <Table columns={proxyColumns} rows={proxies} rowKey={(proxy) => (proxy as Proxy).id} loading={loading}>
    {#snippet cell({ column, row })}
      {@const proxy = row as Proxy}
      {#if column.key === 'url'}
        <HStack gap={2} align="center">
          {#if proxy.location}<Chip text={proxy.location} size="small" />{/if}
          <Text variant="value" mono truncate>{proxy.url}</Text>
        </HStack>
      {:else if column.key === 'latency'}
        <Text size="sm">{formatLatency(proxy.latency)}</Text>
      {:else if column.key === 'score'}
        <Text size="sm">{proxy.score.toFixed(2)}</Text>
      {/if}
    {/snippet}
    {#snippet card({ row })}
      {@const proxy = row as Proxy}
      <HStack gap={2} align="center">
        {#if proxy.location}<Chip text={proxy.location} size="small" />{/if}
        <Text variant="value" mono truncate>{proxy.url}</Text>
      </HStack>
      <Text variant="caption">{t('proxy.latency')}: {formatLatency(proxy.latency)} · {t('proxy.score')}: {proxy.score.toFixed(2)}</Text>
    {/snippet}
    {#snippet empty()}
      <EmptyState title={t('proxy.pool.empty')} />
    {/snippet}
  </Table>
{/snippet}

{#snippet recheckBody()}
  <VStack gap={4}>
    <Text variant="subtitle" text={t('proxy.recheck_banned.desc')} />
    <Grid min="md" gap={4}>
      <Field label={t('proxy.recheck_banned.reason')}>
        <Select value={recheckReason} options={recheckReasonOptions} onchange={(value) => { recheckReason = value }} />
      </Field>
      <Field label={t('proxy.recheck_banned.source')}>
        <Select value={recheckSource} options={recheckSourceOptions} onchange={(value) => { recheckSource = value }} searchable={true} />
      </Field>
    </Grid>
    <HStack gap={3} align="center" wrap>
      <Button style="prominent" disabled={recheckingBanned} onclick={recheckBanned}>
        {recheckingBanned ? t('proxy.pool.refreshing') : t('proxy.recheck_banned.action')}
      </Button>
      {#if recheckQueued !== null}
        <Text variant="caption">{t('proxy.recheck_banned.queued')}: {recheckQueued}. {t('proxy.recheck_banned.success')}</Text>
      {/if}
    </HStack>
  </VStack>
{/snippet}

<VStack gap={6}>
  <Header title={t('proxy.title')} info={t('proxy.pool.auto_refresh')}>
    {#snippet actions()}
      {#if tab !== 'pools'}
        <ToolbarItem primary>
                  <Button
            style="prominent"
            icon={{ name: 'refresh' }}
            disabled={requestingRefresh}
            onclick={refreshPool}
          >{requestingRefresh ? t('proxy.pool.refreshing') : t('proxy.pool.refresh')}</Button>
        </ToolbarItem>
      {/if}
    {/snippet}
  </Header>

  {#if error}
    <Banner variant="error" text={error} />
  {/if}

  {#if status?.last_error}
    <Banner variant="error" text={status.last_error} />
  {/if}

  <Picker
    value={tab}
    onchange={(next) => ontabchange?.(next as ProxyTab)}
    options={[
      { value: 'status', label: t('proxy.pool.status') },
      { value: 'sources', label: t('proxy.sources.title') },
      { value: 'pools', label: t('proxy.custom_pools.title') }
    ]}
    ariaLabel={t('proxy.title')}
  />

  {#if loading && !status}
    <EmptyState title={t('common.state.loading')} />
  {:else if tab === 'status'}
    {#if status}
      <SectionCard>
        <Grid min="xs" gap={4}>
          <Stat label={t('proxy.pool.mode')}>
            <Chip text={status.mode} color="chip-accent" size="small" />
          </Stat>
          <Stat label={t('proxy.pool.net')} value={status.net.rtt_ms > 0 ? `${status.net.state} ${status.net.rtt_ms} ms` : status.net.state} />
          <Stat label={`${t('proxy.pool.inflight')} / ${t('proxy.pool.limit')}`}>
            <Progress value={status.limit > 0 ? status.inflight / status.limit : 0} tone={status.limit > 0 && status.inflight >= status.limit ? 'warning' : 'accent'}>
              {#snippet label()}<Text size="sm" text={loadLabel} />{/snippet}
            </Progress>
          </Stat>
          <Stat label={t('proxy.pool.total')} value={status.total} />
        </Grid>
        <StackedBar
          items={[
            { label: `${t('proxy.pool.alive')} · ${status.alive}`, value: status.alive, tone: 'success' },
            { label: `${t('proxy.pool.suspect')} · ${status.suspect}`, value: status.suspect, tone: 'warning' },
            { label: `${t('proxy.pool.banned')} · ${status.banned}`, value: status.banned, tone: 'danger' },
          ]}
        />
        {#if hasBanReasons}
          <HStack gap={3} wrap>
            <Text variant="label">{t('proxy.pool.ban_reasons')}</Text>
            <Text size="sm" mono>{formatBanReasons(status.ban_reasons)}</Text>
          </HStack>
        {/if}
      </SectionCard>

      <VStack gap={4}>
        <Header level="section" title={t('proxy.pool.lanes')} />
        <Table columns={laneColumns} rows={status.lanes} rowKey={(lane) => lane.lane}>
          {#snippet cell({ column, row })}
            {@const lane = row as ProxyStatus['lanes'][number]}
            {#if column.key === 'lane'}
              <Text variant="value">{lane.lane}</Text>
            {:else if column.key === 'inflight'}
              <Text size="sm">{lane.inflight}</Text>
            {:else if column.key === 'queued'}
              <Text size="sm">{lane.queued}</Text>
            {/if}
          {/snippet}
          {#snippet card({ row })}
            {@const lane = row as ProxyStatus['lanes'][number]}
            <Text variant="value">{lane.lane}</Text>
            <Text variant="caption">{t('proxy.pool.inflight')}: {lane.inflight} · {t('proxy.pool.queued')}: {lane.queued}</Text>
          {/snippet}
          {#snippet empty()}
            <EmptyState title={t('proxy.pool.empty')} />
          {/snippet}
        </Table>
      </VStack>

      <!-- rarely needed: folded away so the overview stays short -->
      <Accordion multiple bind:value={openSections} items={[
        { id: 'proxies', title: `${t('proxy.pool.healthy')} (${proxies.length})`, body: healthyBody },
        { id: 'recheck', title: t('proxy.recheck_banned.title'), body: recheckBody },
      ]} />
    {:else}
      <EmptyState title={t('common.state.loading')} />
    {/if}
  {:else if tab === 'sources'}
    <VStack gap={4}>
      <Header level="section" title={t('proxy.sources.title')} subtitle={t('proxy.sources.desc')} />
      <Table
        columns={sourceColumns}
        rows={mergedSources}
        rowKey={(row) => (row as MergedSource).name}
        loading={loading}
      >
        {#snippet cell({ column, row })}
          {@const src = row as MergedSource}
          {#if column.key === 'name'}
            <Text variant="value">{src.name}</Text>
            {#if src.cfg}<Text variant="caption" class="src-key">{src.cfg.key}</Text>{/if}
            {#if src.cfg?.last_error}<Text tone="danger" variant="caption">{src.cfg.last_error}</Text>{/if}
          {:else if column.key === 'fetched'}
            {@const lf = src.cfg?.last_fetch_at}{#if lf && !lf.startsWith('0001-')}<RelativeTime date={lf} locale={locale()} />{:else}<Text size="sm">—</Text>{/if}
          {:else if column.key === 'candidates'}
            <Text size="sm">{src.cfg ? src.cfg.total : '—'}</Text>
            {#if src.cfg && src.cfg.unsupported > 0}<Text variant="caption">{t('proxy.sources.unsupported')}: {src.cfg.unsupported}</Text>{/if}
          {:else if column.key === 'alive'}
            <Text size="sm" tone="success">{src.live?.alive ?? '—'}</Text>
          {:else if column.key === 'suspect'}
            <Text size="sm" tone="warning">{src.live?.suspect ?? '—'}</Text>
          {:else if column.key === 'banned'}
            <span title={src.live ? formatBanReasons(src.live.ban_reasons) : ''}><Text size="sm" tone="danger">{src.live?.banned ?? '—'}</Text></span>
          {/if}
        {/snippet}
        {#snippet card({ row })}
          {@const src = row as MergedSource}
          <Text variant="value">{src.name}</Text>
          {#if src.cfg}<Text variant="caption" class="src-key">{src.cfg.key}</Text>{/if}
          {#if src.live}
            <Text variant="caption">{t('proxy.pool.alive')}: {src.live.alive} · {t('proxy.pool.suspect')}: {src.live.suspect} · {t('proxy.pool.banned')}: {src.live.banned}</Text>
          {/if}
          {#if src.cfg}
            <Text variant="caption">{t('proxy.sources.last_fetched')}: {formatTime(src.cfg.last_fetch_at)} · {t('proxy.sources.candidates')}: {src.cfg.total} · {t('proxy.sources.unsupported')}: {src.cfg.unsupported}</Text>
          {/if}
          {#if src.live && formatBanReasons(src.live.ban_reasons)}<Text size="sm" mono>{formatBanReasons(src.live.ban_reasons)}</Text>{/if}
          {#if src.cfg?.last_error}<Text tone="danger" variant="caption">{src.cfg.last_error}</Text>{/if}
        {/snippet}
        {#snippet empty()}
          <EmptyState title={t('proxy.no_sources')} icon="extension" />
        {/snippet}
      </Table>
    </VStack>
  {:else}
    <VStack gap={4}>
      <Header level="section" title={t('proxy.custom_pools.title')} subtitle={t('proxy.custom_pools.desc')}>
        {#snippet actions()}
          <Button style="prominent" onclick={() => openPoolModal('create')} icon={{ name: 'add' }}>{t('proxy.custom_pools.new')}</Button>
        {/snippet}
      </Header>
      <Table
        columns={poolColumns}
        rows={pools}
        rowKey={(pool) => (pool as ProxyPool).id}
        loading={loading}
        hideHeaderWhenEmpty
      >
        {#snippet cell({ column, row })}
          {@const pool = row as ProxyPool}
          {#if column.key === 'name'}
            <Text variant="value">{pool.name}</Text>
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
                tint="var(--fui-color-danger)"
                size="small"
                icon={{ name: 'delete' }}
                title={t('common.actions.delete')}
                ariaLabel={t('common.actions.delete')}
                onclick={(e) => openDeletePool(pool, e.currentTarget as HTMLElement)}
              />
            </HStack>
          {/if}
        {/snippet}
        {#snippet card({ row })}
          {@const pool = row as ProxyPool}
          <HStack gap={3} align="center">
            <Text variant="value">{pool.name}</Text>
            <Spacer />
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
                tint="var(--fui-color-danger)"
                size="small"
                icon={{ name: 'delete' }}
                title={t('common.actions.delete')}
                ariaLabel={t('common.actions.delete')}
                onclick={(e) => openDeletePool(pool, e.currentTarget as HTMLElement)}
              />
            </HStack>
          </HStack>
          <Text variant="caption">{t('proxy.title')}: {pool.entries.length}</Text>
        {/snippet}
        {#snippet empty()}
          <EmptyState title={t('proxy.custom_pools.empty')} />
        {/snippet}
      </Table>
    </VStack>
  {/if}

  <FloatingView
    open={Boolean(deletePoolTarget)}
    anchor={deletePoolAnchor}
    width="sm"
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

<style>
  :global(.src-key) {
    overflow-wrap: anywhere;
  }
</style>
