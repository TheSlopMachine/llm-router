<script lang="ts">
  import { VStack, HStack, Text, Button, Table, TextEdit, SectionCard, List, Spacer, Chip, Box } from '$ui'
  import type { TableColumn } from '$ui'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { modal } from '$lib/modal.svelte'
  import { getErrorMessage } from '$lib/errors'
  import type { Proxy, ProxyStatus, ProxySourceInfo } from '$lib/types'
  import { t, n } from '$lib/i18n.svelte'

  let proxies = $state<Proxy[]>([])
  let sources = $state<ProxySourceInfo[]>([])
  let status = $state<ProxyStatus | null>(null)
  let loading = $state(true)
  let error = $state('')

  let newUrl = $state('')
  let newLocation = $state('')
  let adding = $state(false)

  let manualProxies = $derived(proxies.filter((p) => p.source === 'manual'))
  let sourcesActive = $derived(sources.some((s) => s.status !== 'idle'))
  let poolChecking = $derived(status?.checking ?? false)
  let poolBusy = $derived(sourcesActive || poolChecking)

  const proxyColumns: TableColumn[] = [
    { key: 'url', title: t('URL'), width: '1fr' },
    { key: 'protocol', title: t('Protocol'), width: '80px' },
    { key: 'location', title: t('Location'), width: '80px' },
    { key: 'ping', title: t('Ping'), width: '80px', align: 'right' },
    { key: 'speed', title: t('Speed'), width: '80px', align: 'right' },
    { key: 'actions', title: '', width: '44px', align: 'right' },
  ]

  onMount(() => {
    loadAll()
    const poll = setInterval(() => {
      if (poolBusy) {
        reloadSources()
      }
    }, 2000)
    return () => clearInterval(poll)
  })

  async function loadAll(): Promise<void> {
    loading = true
    error = ''
    try {
      const [p, s, st] = await Promise.all([api.proxies.list(), api.proxies.sources(), api.proxies.status()])
      proxies = p
      sources = s
      status = st
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function reloadPool(): Promise<void> {
    proxies = await api.proxies.list()
    status = await api.proxies.status()
  }

  async function reloadSources(): Promise<void> {
    try {
      sources = await api.proxies.sources()
      status = await api.proxies.status()
    } catch {
      // Polling is best-effort
    }
  }

  async function addProxy(): Promise<void> {
    const url = newUrl.trim()
    if (!url) return
    adding = true
    error = ''
    try {
      await api.proxies.add(url, newLocation.trim().toUpperCase())
      newUrl = ''
      newLocation = ''
      await reloadPool()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      adding = false
    }
  }

  async function deleteProxy(p: Proxy): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('Delete proxy'),
      message: `${t('Remove')} ${p.url} ${t('from the pool?')}`,
      severity: 'medium',
      confirmText: t('Delete'),
      confirmRole: 'destructive',
    })
    if (!confirmed) return
    try {
      await api.proxies.delete(p.id)
      proxies = proxies.filter((x) => x.id !== p.id)
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function refreshSource(s: ProxySourceInfo): Promise<void> {
    try {
      await api.proxies.refreshSource(s.key)
      await reloadSources()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function openSource(s: ProxySourceInfo): void {
    window.location.hash = `#/proxy/source/${encodeURIComponent(s.key)}`
  }
</script>

<VStack gap={6}>
  <VStack gap={1}>
    <Text tag="h1" size="xl" weight="bold">{t('Proxies')}</Text>
    <Text tone="soft" size="sm">{t('Manage proxies and sources for provider requests.')}</Text>
  </VStack>

  {#if error}
    <Text tone="danger" size="sm">{error}</Text>
  {/if}

  <SectionCard title="Proxy Sources" description="Plugins providing dynamic proxy lists.">
    {#if sources.length === 0}
      <Text tone="soft" size="sm">{t('No proxy list sources installed. Install a proxy-source plugin (e.g. proxifly).')}</Text>
    {:else}
      <List>
        {#each sources as s (s.key)}
          <div style="padding: var(--space-4);">
            <HStack align="center" gap={4}>
              <VStack gap={1} grow>
                <Text weight="bold" size="base">{s.key}</Text>
                <HStack gap={2} align="center">
                  <Text size="xs" tone="soft">{n(s.total, 'proxy', 'proxies', 'прокси', 'прокси', 'прокси')}</Text>
                  {#if s.status !== 'idle'}
                    <Chip text={t(s.status)} color="chip-accent" size="small" />
                  {/if}
                </HStack>
              </VStack>
              <Spacer />
              <HStack gap={2}>
                <Button
                  style="none"
                  size="small"
                  icon={{ name: 'refresh' }}
                  text={t('Fetch')}
                  disabled={s.status !== 'idle'}
                  onclick={() => refreshSource(s)}
                />
                <Button
                  style="text"
                  size="small"
                  icon={{ name: 'chevron_right', placement: 'right' }}
                  text={t('Details')}
                  onclick={() => openSource(s)}
                />
              </HStack>
            </HStack>
          </div>
        {/each}
      </List>
    {/if}
  </SectionCard>

  <SectionCard title="Add Manual Proxy" description="Directly add a proxy server to the pool.">
    <HStack gap={4} align="end">
      <VStack gap={1} grow>
        <Text size="xs" weight="medium" tone="soft" tag="label">{t('Proxy URL')}</Text>
        <TextEdit bind:value={newUrl} hint="http://user:pass@host:port" />
      </VStack>
      <VStack gap={1} style="width: 120px;">
        <Text size="xs" weight="medium" tone="soft" tag="label">{t('Location')}</Text>
        <TextEdit bind:value={newLocation} hint="US" />
      </VStack>
      <Button style="prominent" onclick={addProxy} disabled={adding || !newUrl.trim()}>{adding ? t('Adding…') : t('Add')}</Button>
    </HStack>
  </SectionCard>

  <VStack gap={4}>
    <HStack align="center">
      <Text tag="h2" size="md" weight="bold">{t('Proxy Pool')}</Text>
      <Spacer />
      {#if status}
        <HStack gap={3} align="center">
          {#if status.searching}<Chip text={t('searching')} color="chip-accent" size="small" icon="search" />{/if}
          {#if status.checking}<Chip text={t('checking')} color="chip-accent" size="small" icon="network_check" />{/if}
          <Text size="sm" tone="soft">{t('Pooled')}: {status.total}</Text>
        </HStack>
      {/if}
    </HStack>

    <Table
      columns={proxyColumns}
      rows={proxies}
      rowKey={(p) => (p as Proxy).id}
      loading={loading}
    >
      {#snippet cell({ column, row })}
        {@const p = row as Proxy}
        {#if column.key === 'url'}
          <VStack gap={0}>
            <Text size="base" weight="medium" mono truncate>{p.url}</Text>
            <Text size="xs" tone="soft">{p.source}</Text>
          </VStack>
        {:else if column.key === 'protocol'}
          <Text size="sm">{p.protocol.toUpperCase()}</Text>
        {:else if column.key === 'location'}
          <Text size="sm" weight="bold">{p.location || '—'}</Text>
        {:else if column.key === 'ping'}
          <Text size="sm" tone={p.handshake_ms > 0 ? (p.handshake_ms < 500 ? 'success' : 'warning') : 'soft'}>
            {p.handshake_ms > 0 ? `${p.handshake_ms}ms` : '—'}
          </Text>
        {:else if column.key === 'speed'}
          <Text size="sm" tone={p.speed_kbps > 0 ? 'default' : 'soft'}>
            {p.speed_kbps > 0 ? `${(p.speed_kbps / 1024).toFixed(1)}MB/s` : '—'}
          </Text>
        {:else if column.key === 'actions'}
          <Button
            style="text"
            tint="#dc2626"
            size="small"
            icon={{ name: 'delete' }}
            onclick={() => deleteProxy(p)}
            ariaLabel={t('Delete proxy')}
          />
        {/if}
      {/snippet}
      {#snippet empty()}
        <Text tone="soft" size="sm" align="center">{t('No proxies in the pool.')}</Text>
      {/snippet}
    </Table>
  </VStack>
</VStack>
