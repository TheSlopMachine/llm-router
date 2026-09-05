<script lang="ts">
  import Dropdown from './Dropdown.svelte'
  import type { MetricsFilters, Provider } from '../lib/types'

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

  const timeRangeOptions = [
    { value: 'hour', label: 'Last Hour' },
    { value: '1d', label: '1 Day' },
    { value: '7d', label: '7 Days' },
    { value: '28d', label: '28 Days' },
    { value: '90d', label: '90 Days' },
    { value: 'month', label: 'This Month' }
  ]

  let providerOptions = $derived([
    { value: '', label: 'All Providers' },
    ...providers.map((p: Provider) => ({ value: p.id, label: p.name }))
  ])

  let modelOptions = $derived([
    { value: '', label: 'All models' },
    ...models.map((m: string) => ({ value: m, label: m }))
  ])
</script>

<div class="filters">
  <div class="filter-group">
    <label for="filter-provider">Provider</label>
    <Dropdown
      bind:value={filters.provider_id}
      options={providerOptions}
      onchange={handleChange}
      rounded="sm"
    />
  </div>

  <div class="filter-group">
    <label for="filter-time-range">Time Range</label>
    <Dropdown
      bind:value={filters.time_range}
      options={timeRangeOptions}
      onchange={handleChange}
      rounded="sm"
    />
  </div>

  <div class="filter-group">
    <label for="filter-model">Model</label>
    <Dropdown
      bind:value={filters.model}
      options={modelOptions}
      onchange={handleChange}
      rounded="sm"
    />
  </div>
</div>

<style>
  .filters {
    display: flex;
    gap: 24px;
    margin-bottom: 32px;
  }
  .filter-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .filter-group label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
  }
  .filter-group :global(.dropdown) {
    min-width: 180px;
  }
</style>
