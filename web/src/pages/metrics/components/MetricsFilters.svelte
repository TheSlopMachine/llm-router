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
    { value: 'hour', label: t('Last Hour') },
    { value: '1d', label: t('1 Day') },
    { value: '7d', label: t('7 Days') },
    { value: '28d', label: t('28 Days') },
    { value: '90d', label: t('90 Days') },
    { value: 'month', label: t('This Month') }
  ])

  let providerOptions = $derived([
    { value: '', label: t('All Providers') },
    ...providers.map((p: Provider) => ({ value: p.id, label: p.name }))
  ])

  let modelOptions = $derived([
    { value: '', label: t('All models') },
    ...models.map((m: string) => ({ value: m, label: m }))
  ])
</script>

<HStack gap={6} align="end">
  <VStack gap={1} grow>
    <Text size="sm" weight="medium" tone="soft" tag="label">{t('Provider')}</Text>
    <Select
      bind:value={filters.provider_id}
      options={providerOptions}
      onchange={handleChange}
    />
  </VStack>

  <VStack gap={1} grow>
    <Text size="sm" weight="medium" tone="soft" tag="label">{t('Time Range')}</Text>
    <Select
      bind:value={filters.time_range}
      options={timeRangeOptions}
      onchange={handleChange}
    />
  </VStack>

  <VStack gap={1} grow>
    <Text size="sm" weight="medium" tone="soft" tag="label">{t('Model')}</Text>
    <Select
      bind:value={filters.model}
      options={modelOptions}
      onchange={handleChange}
    />
  </VStack>
</HStack>
