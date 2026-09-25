<script lang="ts">
  import { VStack, HStack, Text, Button, Table, Spacer, Chip } from '$ui'
  import type { TableColumn } from '$ui'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { Proxy, ProxySourceInfo } from '$lib/types'
  import { t, n } from '$lib/i18n.svelte'

  let { sourceKey }: { sourceKey: string } = $props()

  const PAGE_SIZE = 100

  let info = $state<ProxySourceInfo | null>(null)
  let items = $state<Proxy[]>([])
  let total = $state(0)
  let loading = $state(true)
  let loadingMore = $state(false)
  let error = $state('')

  let hasMore = $derived(items.length < total)
  let active = $derived(info !== null && info.status !== 'idle')

  const proxyColumns: TableColumn[] = [
    { key: 'url', title: t('URL'), width: '1fr' },
    { key: 'protocol', title: t('Protocol'), width: '100px' },
    { key: 'location', title: t('Location'), width: '100px' },
  ]

  onMount(() => {
    load()
    const poll = setInterval(() => {
      if (active) {
        reloadInfo()
        reloadItems(false)
      }
    }, 2000)
    return () => clearInterval(poll)
  })

  async function load(): Promise<void> {
    loading = true
    error = ''
    try {
      await Promise.all([reloadInfo(), reloadItems(false)])
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function reloadInfo(): Promise<void> {
    const sources = await api.proxies.sources()
    info = sources.find((s) => s.key === sourceKey) ?? null
  }

  async function reloadItems(append: boolean): Promise<void> {
    const offset = append ? items.length : 0
    const page = await api.proxies.sourceProxies(sourceKey, offset, PAGE_SIZE)
    total = page.total
    items = append ? [...items, ...page.items] : page.items
  }

  async function showMore(): Promise<void> {
    loadingMore = true
    try {
      await reloadItems(true)
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loadingMore = false
    }
  }

  function back(): void {
    window.location.hash = '#/proxy'
  }
</script>

<VStack gap={6}>
  <HStack gap={4} align="center">
    <Button onclick={back} title={t('Back')} ariaLabel={t('Back')} icon={{ name: 'arrow_back' }} />
    {#if info}
      <VStack gap={1}>
        <Text tag="h1" size="lg" weight="bold">{info.name}</Text>
        <Text size="xs" tone="soft">{info.key}</Text>
        <HStack gap={2} align="center">
          {#if info.status === 'fetching'}
            <Text tone="accent" size="sm">{t('Fetching list…')}</Text>
          {:else if info.status === 'adding'}
            <Text tone="accent" size="sm">{t('Adding proxies…')}</Text>
          {:else if info.status === 'rotating'}
            <Text tone="accent" size="sm">{t('Checking proxies…')}</Text>
          {:else}
            <Text tone="soft" size="sm">{t('Idle')}</Text>
          {/if}
          <Text tone="soft" size="sm">· {n(info.total, 'fetched', 'fetched', 'загружено', 'загружено', 'загружено')}</Text>
          <Text tone="soft" size="sm">· {n(info.pooled, 'pooled', 'pooled', 'в пуле', 'в пуле', 'в пуле')}</Text>
        </HStack>
      </VStack>
    {/if}
  </HStack>

  {#if loading}
    <Text tone="soft" align="center">{t('Loading…')}</Text>
  {:else if !info}
    <VStack align="center" gap={4}>
      <Text tone="soft">{t('Proxy source not found.')}</Text>
      <Button onclick={back}>{t('Back')}</Button>
    </VStack>
  {:else}
    {#if error}
      <Text tone="danger" size="sm">{error}</Text>
    {/if}

    <VStack gap={4}>
      <Table
        columns={proxyColumns}
        rows={items}
        rowKey={(p) => (p as Proxy).id}
        loading={false}
      >
        {#snippet cell({ column, row })}
          {@const p = row as Proxy}
          {#if column.key === 'url'}
            <Text size="base" weight="medium" mono truncate>{p.url}</Text>
          {:else if column.key === 'protocol'}
            <Text size="sm">{p.protocol.toUpperCase()}</Text>
          {:else if column.key === 'location'}
            <Text size="sm" weight="bold">{p.location || '—'}</Text>
          {/if}
        {/snippet}
        {#snippet empty()}
          <Text tone="soft" size="sm" align="center">{t('No proxies fetched from this source.')}</Text>
        {/snippet}
      </Table>

      {#if hasMore}
        <HStack justify="center">
          <Button onclick={showMore} disabled={loadingMore}>
            {loadingMore ? t('Loading…') : t('Show more')}
          </Button>
        </HStack>
      {/if}
    </VStack>
  {/if}
</VStack>
