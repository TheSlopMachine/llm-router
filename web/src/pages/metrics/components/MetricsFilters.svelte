<script lang="ts">
  import { Select, HStack, VStack, Text } from '$ui'
  import type { MetricsFilters, Provider } from '$lib/types'
  import { t } from '$lib/i18n.svelte'

  type Filters = MetricsFilters & { provider_id: string; model: string; time_range: string }

  let {
    filters = $bindable({ provider_id: '', model: '', time_range: 'hour' } as unknown as Filters),
    providers = [],
    models = [],
    onchange
  } = $props<{
    filters: Filters
    providers: Provider[]
    models: string[]
    onchange?: (f: Filters) => void
  }>()

  function handleChange() {
    onchange?.(filters)
  }

  let timeRangeOptions = $derived([
    { value: 'hour', label: t('metrics.range.last_hour') },
    { value: '1d', label: t('metrics.range.1day') },
    { value: '7d', label: t('metrics.range.7days') },
    { value: '28d', label: t('metrics.range.28days') },
    { value: '90d', label: t('metrics.range.90days') },
    { value: 'month', label: t('metrics.range.this_month') }
  ])

  let providerOptions = $derived([
    { value: '', label: t('providers.list.title') },
    ...providers.map((p: Provider) => ({ value: p.id, label: p.name }))
  ])

  let modelOptions = $derived([
    { value: '', label: t('models.list.all') },
    ...models.map((m: string) => ({ value: m, label: m }))
  ])
</script>

<HStack gap={3} align="end" wrap>
  <VStack gap={1} fill="sm">
    <Text size="sm" weight="medium" tone="soft" tag="label">{t('providers.detail.title')}</Text>
    <Select
      bind:value={filters.provider_id}
      options={providerOptions}
      onchange={handleChange}
    />
  </VStack>

  <VStack gap={1} fill="sm">
    <Text size="sm" weight="medium" tone="soft" tag="label">{t('metrics.time_range')}</Text>
    <Select
      bind:value={filters.time_range}
      options={timeRangeOptions}
      onchange={handleChange}
    />
  </VStack>

  <VStack gap={1} fill="sm">
    <Text size="sm" weight="medium" tone="soft" tag="label">{t('models.list.title')}</Text>
    <Select
      bind:value={filters.model}
      options={modelOptions}
      onchange={handleChange}
    />
  </VStack>
</HStack>
