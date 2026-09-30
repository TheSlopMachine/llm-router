<script lang="ts">
  import { VStack, HStack, Text, Button, Table, SectionCard, List, Spacer, Chip } from '$ui'
  import type { TableColumn } from '$ui'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { Proxy, ProxyStatus, ProxySourceInfo } from '$lib/types'
  import { t } from '$lib/i18n.svelte'

  let proxies = $state<Proxy[]>([])
  let sources = $state<ProxySourceInfo[]>([])
  let status = $state<ProxyStatus | null>(null)
  let loading = $state(true)
  let requestingRefresh = $state(false)
  let error = $state('')

  const proxyColumns: TableColumn[] = [
    { key: 'url', title: t('URL'), width: '1fr', priority: 1 },
    { key: 'latency', title: t('Latency'), width: '90px', align: 'right', priority: 2 },
    { key: 'score', title: t('Score'), width: '80px', align: 'right', priority: 2 },
  ]

  onMount(() => {
    void loadAll()
    const poll = setInterval(() => void loadAll(), 5000)
    return () => clearInterval(poll)
  })

  async function loadAll(): Promise<void> {
    try {
      const [nextProxies, nextSources, nextStatus] = await Promise.all([
        api.proxies.list(),
        api.proxies.sources(),
        api.proxies.status(),
      ])
      proxies = nextProxies
      sources = nextSources
      status = nextStatus
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

  function formatLatency(nanoseconds: number): string {
    return `${(nanoseconds / 1_000_000).toFixed(0)}ms`
  }

  function formatMinutes(nanoseconds: number): string {
    const minutes = Math.round(nanoseconds / 60_000_000_000)
    return `${minutes} ${t('min')}`
  }

  function formatTime(value?: string): string {
    if (!value || value.startsWith('0001-')) return '—'
    return new Date(value).toLocaleString()
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
    <Text tone="danger" size="sm">{error}</Text>
  {/if}

  {#if status?.last_error}
    <Text tone="danger" size="sm">{status.last_error}</Text>
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

  <SectionCard title={t('Proxy sources')} description={t('Plugins provide candidate URLs. The library retains and checks them.')}>
    {#if sources.length === 0}
      <Text tone="soft" size="sm">{t('No proxy list sources installed. Install a proxy-source plugin (e.g. proxifly).')}</Text>
    {:else}
      <List>
        {#each sources as source (source.key)}
          <div style="padding: var(--space-4);">
            <VStack gap={2}>
              <HStack align="center" gap={3}>
                <VStack gap={0} grow>
                  <Text weight="bold" size="base">{source.name}</Text>
                  <Text size="xs" tone="soft">{source.key}</Text>
                </VStack>
                <Text size="sm" tone="soft">{t('Candidates returned')}: {source.total}</Text>
              </HStack>
              <HStack gap={3} wrap>
                <Text size="xs" tone="soft">{t('Last fetched')}: {formatTime(source.last_fetch_at)}</Text>
                <Text size="xs" tone="soft">{t('Unsupported')}: {source.unsupported}</Text>
              </HStack>
              {#if source.last_error}
                <Text size="xs" tone="danger">{source.last_error}</Text>
              {/if}
            </VStack>
          </div>
        {/each}
      </List>
    {/if}
  </SectionCard>

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
        <Text tone="soft" size="sm" align="center">{t('No healthy proxies in the pool.')}</Text>
      {/snippet}
    </Table>
  </VStack>
</VStack>
