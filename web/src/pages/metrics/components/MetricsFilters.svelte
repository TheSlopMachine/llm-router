<script lang="ts">
  import { Field, FormRow, Picker } from '@fuckyouryan/fui'
  import type { MetricsFilters, Provider } from '$lib/types'
  import { t } from '$lib/i18n.svelte'

  type Filters = MetricsFilters & { provider_id: string; model: string; time_range: string }
  type FilterField = 'provider_id' | 'time_range' | 'model'

  let {
    filters = $bindable({ provider_id: '', model: '', time_range: 'hour' } as unknown as Filters),
    providers = [],
    models = [],
    onchange
  }: {
    filters: Filters
    providers: Provider[]
    models: string[]
    onchange?: (f: Filters, field: FilterField) => void
  } = $props()

  function handleChange(field: FilterField) {
    onchange?.(filters, field)
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

<FormRow>
  <Field label={t('providers.detail.title')}>
    <Picker
      bind:value={filters.provider_id}
      options={providerOptions}
      ariaLabel={t('providers.detail.title')}
      onchange={() => handleChange('provider_id')}
    />
  </Field>

  <Field label={t('metrics.time_range')}>
    <Picker
      bind:value={filters.time_range}
      options={timeRangeOptions}
      ariaLabel={t('metrics.time_range')}
      onchange={() => handleChange('time_range')}
    />
  </Field>

  <Field label={t('models.list.title')}>
    <Picker
      bind:value={filters.model}
      options={modelOptions}
      ariaLabel={t('models.list.title')}
      onchange={() => handleChange('model')}
    />
  </Field>
</FormRow>
