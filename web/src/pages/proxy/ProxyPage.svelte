<script lang="ts">
  import SectionHeading from '../../components/SectionHeading.svelte'
  import { VStack, HStack, Text, Button, Toggle, Table, Card, Chip, FloatingView, Banner, ConfirmAction, Header, Stat, Grid, Picker, Spacer, RelativeTime, Field, TextField, toast, type TableColumn, ContentUnavailable, type HeaderAction } from '@fuckyouryan/fui'
  import { viewport } from '$lib/viewport.svelte'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { openFormModal } from '$lib/modal-helpers'
  import { getErrorMessage } from '$lib/errors'
    import type { ProxyStatus, ProxySourceInfo, ProxyPool, RouterConfiguration } from '$lib/types'
  import { t, tx, locale, type TranslationKey } from '$lib/i18n.svelte'
  import CustomPoolModal from './components/CustomPoolModal.svelte'
  import HealthyProxies from './components/HealthyProxies.svelte'

  export type ProxyTab = 'auto' | 'pools'

  let { tab = 'auto', ontabchange }: {
    tab?: ProxyTab
    ontabchange?: (next: ProxyTab) => void
  } = $props()

  let sources = $state<ProxySourceInfo[]>([])
  let status = $state.raw<ProxyStatus | null>(null)
  let lastStatusJson = ''
  let pools = $state<ProxyPool[]>([])
  let loading = $state(true)
  let requestingRefresh = $state(false)
  let recheckingBanned = $state(false)
  let forcing = $state(false)
  let routerConfig = $state<RouterConfiguration | null>(null)
  let maxChecksInput = $state('0')
  let maxChecksError = $state('')
  let savingMaxChecks = $state(false)
  let savingSwitch = $state(false)
  const proxyEnabled = $derived(routerConfig?.proxy_enabled ?? true)
  const poolEnabled = $derived(routerConfig?.proxy_pool_enabled ?? true)
  const MAX_CHECKS_LIMIT = 2000
  let error = $state('')
  let deletePoolTarget = $state<ProxyPool | null>(null)
  let deletePoolAnchor = $state<HTMLElement>()
  let deletingPool = $state(false)

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
  interface MergedSource { name: string; configuration: ProxySourceInfo | null; live: LiveSource | null }
  const mergedSources = $derived.by<MergedSource[]>(() => {
    const live = status?.sources ?? []
    const rows: MergedSource[] = sources.map((source) => ({
      name: source.name,
      configuration: source,
      live: live.find((l) => l.source === source.name) ?? null,
    }))
    for (const l of live) if (!rows.some((r) => r.name === l.source)) rows.push({ name: l.source, configuration: null, live: l })
    return rows
  })

  let proxiesOpen = $state(false)
  const loadLabel = $derived(status ? `${status.inflight} / ${status.limit}` : '')
  // Exactly one candidate lane works at a time: foreground, or background unless a force is active.
  const idleLane = $derived(status ? (status.mode === 'background' && !status.forced_foreground ? 'foreground' : 'background') : '')

  const laneColumns: TableColumn[] = [
    { key: 'lane', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'inflight', title: t('proxy.pool.inflight'), width: 'var(--fui-table-col-md)', align: 'right', priority: 1 },
    { key: 'queued', title: t('proxy.pool.queued'), width: 'var(--fui-table-col-md)', align: 'right', priority: 1 },
  ]

  onMount(() => {
    void loadAll()
    void loadConfig()
    const poll = setInterval(() => {
      if (!document.hidden) void loadStatus()
    }, 2000)
    return () => clearInterval(poll)
  })

  async function loadConfig(): Promise<void> {
    try {
      const config = await api.config.get()
      routerConfig = config
      maxChecksInput = String(config.proxy_max_checks ?? 0)
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function setSwitch(patch: { proxy_enabled?: boolean; proxy_pool_enabled?: boolean }): Promise<void> {
    if (!routerConfig || savingSwitch) return
    savingSwitch = true
    error = ''
    try {
      const payload = { ...routerConfig, ...patch }
      await api.config.update(payload)
      routerConfig = payload
      await loadStatus()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      savingSwitch = false
    }
  }

  async function saveMaxChecks(raw: string): Promise<void> {
    const value = raw.trim()
    maxChecksInput = value
    if (!/^\d+$/.test(value)) {
      maxChecksError = tx('proxy.pool.max_checks_invalid', { max: MAX_CHECKS_LIMIT })
      return
    }
    const next = Number(value)
    if (next > MAX_CHECKS_LIMIT) {
      maxChecksError = tx('proxy.pool.max_checks_too_high', { max: MAX_CHECKS_LIMIT })
      return
    }
    maxChecksError = ''
    if (!routerConfig || next === (routerConfig.proxy_max_checks ?? 0) || savingMaxChecks) return
    savingMaxChecks = true
    try {
      const payload = { ...routerConfig, proxy_max_checks: next }
      await api.config.update(payload)
      routerConfig = payload
      maxChecksInput = String(next)
      await loadStatus()
    } catch (e) {
      maxChecksError = getErrorMessage(e)
    } finally {
      savingMaxChecks = false
    }
  }

  let latestStatusRequest = 0

  async function loadStatus(): Promise<void> {
    const request = ++latestStatusRequest
    try {
      const nextStatus = await api.proxies.status()
      if (request !== latestStatusRequest) return
      const json = JSON.stringify(nextStatus)
      if (json !== lastStatusJson) {
        lastStatusJson = json
        status = nextStatus
      }
      error = ''
    } catch (e) {
      if (request === latestStatusRequest) error = getErrorMessage(e)
    }
  }

  async function loadAll(): Promise<void> {
    try {
      const [nextSources, nextStatus, nextPools] = await Promise.all([
        api.proxies.sources(),
        api.proxies.status(),
        api.proxies.pools.list(),
      ])
      sources = nextSources
      lastStatusJson = JSON.stringify(nextStatus)
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
    error = ''
    try {
      const result = await api.proxies.recheckBanned({})
      toast.success(`${t('proxy.recheck_banned.queued')}: ${result.queued}. ${t('proxy.recheck_banned.success')}`)
      await loadStatus()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      recheckingBanned = false
    }
  }

  async function forceChecks(): Promise<void> {
    forcing = true
    error = ''
    try {
      const result = await api.proxies.forceForeground()
      if (result.started) toast.success(tx('proxy.force.started', { count: result.queued }))
      else toast.error(t('proxy.force.none'))
      await loadStatus()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      forcing = false
    }
  }

  function banReasonLabel(reason: string): string {
    const key = `proxy.ban_reason.${reason}` as TranslationKey
    const label = t(key)
    return label === key ? reason : label
  }

  const banReasonList = $derived(
    Object.entries(status?.ban_reasons ?? {})
      .filter(([, count]) => count > 0)
      .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  )

  function formatTime(value?: string): string {
    if (!value || value.startsWith('0001-')) return '—'
    return new Date(value).toLocaleString()
  }

  function openPoolModal(mode: 'create' | 'edit', pool?: ProxyPool): void {
    error = ''
    openFormModal(CustomPoolModal, {
      title: mode === 'edit' ? t('proxy.custom_pools.edit') : t('proxy.custom_pools.new'),
      size: 'md',
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


  // Primitives dedupe: the header objects are rebuilt only when one of these really changes,
  // not on every 2s status poll.
  const statusLoaded = $derived(status !== null)
  const queuedCount = $derived(status?.queued ?? 0)
  const forcedForeground = $derived(!!status?.forced_foreground)

  // The header's primary slot is the subsystem switch, drawn like the provider
  // enable switch: a power icon, filled when on, soft when off. The open tab
  // adds its actions beside it: pool maintenance on the auto tab while the
  // auto pool runs, pool creation on the custom tab. With proxies off only
  // the switch remains.
  const headerPrimary = $derived<HeaderAction>({
    id: 'proxy-switch',
    text: proxyEnabled ? t('proxy.switch.enabled') : t('proxy.switch.turn_on'),
    icon: 'power_settings_new',
    iconOnly: true,
    selected: proxyEnabled,
    disabled: !routerConfig || savingSwitch,
    onclick: () => void setSwitch({ proxy_enabled: !proxyEnabled }),
  })
  const headerActions = $derived<HeaderAction[]>(
    !proxyEnabled
      ? []
      : tab === 'pools'
        ? [{ id: 'new-pool', text: t('proxy.custom_pools.new'), icon: 'add', onclick: () => openPoolModal('create') }]
        : !poolEnabled
          ? []
          : [
              {
                id: 'force',
                text: t('proxy.force.action'),
                icon: 'bolt',
                loading: forcing || forcedForeground,
                disabled: !statusLoaded || (queuedCount === 0 && !forcedForeground),
                onclick: () => void forceChecks(),
              },
              { id: 'recheck', text: t('proxy.recheck_banned.action'), icon: 'lock_reset', loading: recheckingBanned, onclick: () => void recheckBanned() },
              {
                id: 'refresh',
                text: requestingRefresh ? t('proxy.pool.refreshing') : t('proxy.pool.refresh'),
                icon: 'refresh',
                disabled: requestingRefresh,
                onclick: () => void refreshPool(),
              },
            ],
  )
</script>


<VStack gap={6}>
  <Header title={t('proxy.title')} subtitle={t('proxy.subtitle')} primary={headerPrimary} actions={headerActions} />

  {#if error}
    <Banner tint="danger" text={error} />
  {/if}

  {#if routerConfig && !proxyEnabled}
    <VStack minHeight="50vh" valign="center" halign="center">
      <ContentUnavailable icon="vpn_lock" title={t('proxy.off.title')} />
    </VStack>
  {:else}
  <Picker pickerStyle="segmented"
    value={tab}
    onchange={(next) => ontabchange?.(next as ProxyTab)}
    options={[
      { value: 'auto', label: t('proxy.tab.auto') },
      { value: 'pools', label: t('proxy.custom_pools.title') }
    ]}
    ariaLabel={t('proxy.title')}
  />

  {#if tab === 'auto'}
    <Card>
      <VStack gap={1}>
        <Toggle label={t('proxy.switch.pool')} ariaLabel={t('proxy.switch.pool')} checked={poolEnabled} disabled={!routerConfig || savingSwitch} onchange={(value: boolean) => void setSwitch({ proxy_pool_enabled: value })} />
        <Text variant="caption">{poolEnabled ? t('proxy.pool.auto_refresh') : t('proxy.switch.pool_hint')}</Text>
      </VStack>
    </Card>
  {/if}

  {#if tab === 'auto' && !poolEnabled}
    <!-- auto pool off: its statistics and settings have nothing to show -->
  {:else if loading && !status}
    <ContentUnavailable title={t('common.state.loading')} />
  {:else if tab === 'auto'}
    {#if status?.last_error}
      <Banner tint="danger" text={status.last_error} />
    {/if}
    {#if status}
      <Card>
        <Grid min="xs" gap={5}>
          <Stat label={t('proxy.pool.net')} value={status.net.rtt_ms > 0 ? `${status.net.state} ${status.net.rtt_ms} ms` : status.net.state} />
          <Stat label={`${t('proxy.pool.inflight')} / ${t('proxy.pool.limit')}`}>
            <Text variant="value" text={loadLabel} />
          </Stat>
          <Stat label={t('proxy.pool.total')} value={status.total} />
          <Field label={t('proxy.pool.max_checks')} hint={t('proxy.pool.max_checks_hint')} error={maxChecksError}>
            <TextField value={maxChecksInput} ariaLabel={t('proxy.pool.max_checks')} disabled={savingMaxChecks} onchange={(value) => void saveMaxChecks(value)} />
          </Field>
        </Grid>
        <HStack gap={4} wrap>
          <HStack gap={2} valign="center">
            <span class="legend-dot legend-success"></span>
            <Text size="sm">{t('proxy.pool.alive')} · {status.alive}</Text>
          </HStack>
          <HStack gap={2} valign="center">
            <span class="legend-dot legend-warning"></span>
            <Text size="sm">{t('proxy.pool.suspect')} · {status.suspect}</Text>
          </HStack>
          <HStack gap={2} valign="center">
            <span class="legend-dot legend-danger"></span>
            <Text size="sm">{t('proxy.pool.banned')} · {status.banned}</Text>
          </HStack>
        </HStack>
        {#if banReasonList.length > 0}
          <VStack gap={2}>
            <Text variant="label">{t('proxy.pool.ban_reasons')}</Text>
            <HStack gap={2} wrap>
              {#each banReasonList as [reason, count] (reason)}
                <Chip text={`${banReasonLabel(reason)} · ${count}`} size="sm" />
              {/each}
            </HStack>
          </VStack>
        {/if}
      </Card>

      <VStack gap={4}>
        <SectionHeading title={t('proxy.sources.title')} subtitle={t('proxy.sources.desc')} />
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
              {#if src.configuration?.last_error}<Text tint="danger" variant="caption">{src.configuration.last_error}</Text>{/if}
            {:else if column.key === 'fetched'}
              {@const lf = src.configuration?.last_fetch_at}{#if lf && !lf.startsWith('0001-')}<RelativeTime date={lf} locale={locale()} />{:else}<Text size="sm">—</Text>{/if}
            {:else if column.key === 'candidates'}
              <Text size="sm">{src.configuration ? src.configuration.total : '—'}</Text>
              {#if src.configuration && src.configuration.unsupported > 0}<Text variant="caption">{t('proxy.sources.unsupported')}: {src.configuration.unsupported}</Text>{/if}
            {:else if column.key === 'alive'}
              <Text size="sm" tint="success">{src.live?.alive ?? '—'}</Text>
            {:else if column.key === 'suspect'}
              <Text size="sm" tint="warning">{src.live?.suspect ?? '—'}</Text>
            {:else if column.key === 'banned'}
              <Text size="sm" tint="danger">{src.live?.banned ?? '—'}</Text>
            {/if}
          {/snippet}
          {#snippet card({ row })}
            {@const src = row as MergedSource}
            <Text variant="value">{src.name}</Text>
            {#if src.live}
              <Text variant="caption">{t('proxy.pool.alive')}: {src.live.alive} · {t('proxy.pool.suspect')}: {src.live.suspect} · {t('proxy.pool.banned')}: {src.live.banned}</Text>
            {/if}
            {#if src.configuration}
              <Text variant="caption">{t('proxy.sources.last_fetched')}: {formatTime(src.configuration.last_fetch_at)} · {t('proxy.sources.candidates')}: {src.configuration.total} · {t('proxy.sources.unsupported')}: {src.configuration.unsupported}</Text>
            {/if}
            {#if src.configuration?.last_error}<Text tint="danger" variant="caption">{src.configuration.last_error}</Text>{/if}
          {/snippet}
          {#snippet empty()}
            <ContentUnavailable title={t('proxy.no_sources')} icon="extension" />
          {/snippet}
        </Table>
      </VStack>

      <VStack gap={4}>
        <SectionHeading title={t('proxy.pool.lanes')} />
        <Table columns={laneColumns} rows={status.lanes} rowKey={(lane) => lane.lane}>
          {#snippet cell({ column, row })}
            {@const lane = row as ProxyStatus['lanes'][number]}
            {#if column.key === 'lane'}
              <HStack gap={2} valign="center">
                <Text variant="value">{lane.lane}</Text>
                {#if lane.lane === idleLane}<Text variant="caption">{t('proxy.pool.lane_idle')}</Text>{/if}
              </HStack>
            {:else if column.key === 'inflight'}
              <Text size="sm">{lane.inflight}</Text>
            {:else if column.key === 'queued'}
              <Text size="sm">{lane.lane === idleLane ? '—' : lane.queued}</Text>
            {/if}
          {/snippet}
          {#snippet card({ row })}
            {@const lane = row as ProxyStatus['lanes'][number]}
            <HStack gap={2} valign="center">
              <Text variant="value">{lane.lane}</Text>
              {#if lane.lane === idleLane}<Text variant="caption">{t('proxy.pool.lane_idle')}</Text>{/if}
            </HStack>
            <Text variant="caption">{t('proxy.pool.inflight')}: {lane.inflight} · {t('proxy.pool.queued')}: {lane.lane === idleLane ? '—' : lane.queued}</Text>
          {/snippet}
          {#snippet empty()}
            <ContentUnavailable title={t('proxy.pool.empty')} />
          {/snippet}
        </Table>
      </VStack>

      <!-- rarely needed: folded away so the overview stays short -->
      <Card collapsible title={`${t('proxy.pool.healthy')} (${status.alive})`} bind:open={proxiesOpen}>
        <!-- mounted only while open: it polls -->
        {#if proxiesOpen}<HealthyProxies />{/if}
      </Card>
    {:else}
      <ContentUnavailable title={t('common.state.loading')} />
    {/if}
  {:else}
    <VStack gap={4}>
      <Text variant="caption">{t('proxy.custom_pools.desc')}</Text>
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
            <HStack halign="end" gap={2}>
              <Button
                buttonStyle="text"
                icon={{ name: 'edit' }}
                title={t('common.actions.edit')}
                size="sm"
                ariaLabel={t('common.actions.edit')}
                onclick={() => openPoolModal('edit', pool)}
              />
              <Button
                buttonStyle="text"
                tint="var(--fui-color-danger)"
                size="sm"
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
          <HStack gap={3} valign="center">
            <Text variant="value">{pool.name}</Text>
            <Spacer />
            <HStack halign="end" gap={2}>
              <Button
                buttonStyle="text"
                icon={{ name: 'edit' }}
                title={t('common.actions.edit')}
                size="sm"
                ariaLabel={t('common.actions.edit')}
                onclick={() => openPoolModal('edit', pool)}
              />
              <Button
                buttonStyle="text"
                tint="var(--fui-color-danger)"
                size="sm"
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
          <ContentUnavailable title={t('proxy.custom_pools.empty')} />
        {/snippet}
      </Table>
    </VStack>
  {/if}
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
        oncancel={close}
        onconfirm={confirmDeletePool} />
    {/snippet}
  </FloatingView>
</VStack>

<style>
  .legend-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .legend-success {
    background: springgreen;
  }
  .legend-warning {
    background: yellow;
  }
  .legend-danger {
    background: red;
  }
</style>
