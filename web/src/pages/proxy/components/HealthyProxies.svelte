<script lang="ts">
  import { HStack, Text, Table, Chip, ContentUnavailable, type TableColumn } from '@fuckyouryan/fui'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { formatNanoLatency as formatLatency } from '$lib/format'
  import type { Proxy } from '$lib/types'
  import { t, tx } from '$lib/i18n.svelte'

  // The pool can hold thousands of proxies: render a bounded slice, never the whole list.
  const MAX_ROWS = 200

  let all = $state.raw<Proxy[]>([])
  let loading = $state(true)
  let error = $state('')
  const rows = $derived(all.slice(0, MAX_ROWS))

  const columns: TableColumn[] = [
    { key: 'url', title: t('proxy.url'), width: '1fr', priority: 1 },
    { key: 'latency', title: t('proxy.latency'), width: 'var(--fui-table-col-sm)', align: 'right', priority: 2 },
    { key: 'score', title: t('proxy.score'), width: 'var(--fui-table-col-xs)', align: 'right', priority: 2 },
  ]

  onMount(async () => {
    try {
      all = await api.proxies.list()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  })
</script>

{#if error}
  <Text tint="danger" variant="caption">{error}</Text>
{/if}
<Table {columns} {rows} rowKey={(proxy) => (proxy as Proxy).id} {loading}>
  {#snippet cell({ column, row })}
    {@const proxy = row as Proxy}
    {#if column.key === 'url'}
      <HStack gap={2} valign="center">
        {#if proxy.location}<Chip text={proxy.location} size="sm" />{/if}
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
    <HStack gap={2} valign="center">
      {#if proxy.location}<Chip text={proxy.location} size="sm" />{/if}
      <Text variant="value" mono truncate>{proxy.url}</Text>
    </HStack>
    <Text variant="caption">{t('proxy.latency')}: {formatLatency(proxy.latency)} · {t('proxy.score')}: {proxy.score.toFixed(2)}</Text>
  {/snippet}
  {#snippet empty()}
    <ContentUnavailable title={t('proxy.pool.empty')} />
  {/snippet}
</Table>
{#if all.length > MAX_ROWS}
  <Text variant="caption">{tx('proxy.pool.showing', { shown: MAX_ROWS, total: all.length })}</Text>
{/if}
