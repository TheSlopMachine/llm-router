<script lang="ts">
  import { createEventDispatcher } from 'svelte'
  import Dropdown from './Dropdown.svelte'
  import type { MetricsFilters, Provider } from '../lib/types'
  
  export let filters: MetricsFilters
  export let providers: Provider[] = []
  export let models: string[] = []
  
  const dispatch = createEventDispatcher<{ change: MetricsFilters }>()
  
  function handleChange() {
    dispatch('change', filters)
  }
  
  const timeRangeOptions = [
    { value: 'hour', label: 'Last Hour' },
    { value: '1d', label: '1 Day' },
    { value: '7d', label: '7 Days' },
    { value: '28d', label: '28 Days' },
    { value: '90d', label: '90 Days' },
    { value: 'month', label: 'This Month' },
  ]

  $: providerOptions = [
    { value: '', label: 'All Providers' },
    ...providers.map(p => ({ value: p.id, label: p.name }))
  ]

  $: modelOptions = [
    { value: '', label: 'All models' },
    ...models.map(m => ({ value: m, label: m }))
  ]
</script>

<div class="filters">
  <div class="filter-group">
    <label for="filter-provider">Provider</label>
    <Dropdown
      bind:value={filters.provider_id}
      options={providerOptions}
      on:change={handleChange}
    />
  </div>
  
  <div class="filter-group">
    <label for="filter-time-range">Time Range</label>
    <Dropdown
      bind:value={filters.time_range}
      options={timeRangeOptions}
      on:change={handleChange}
    />
  </div>
  
  <div class="filter-group">
    <label for="filter-model">Model</label>
    <Dropdown
      bind:value={filters.model}
      options={modelOptions}
      on:change={handleChange}
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
