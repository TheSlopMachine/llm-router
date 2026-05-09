<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { api } from '../lib/api'
  import type { MetricsFilters, MetricsOverview, TimeSeriesPoint, Provider } from '../lib/types'
  import MetricsFilters from './MetricsFilters.svelte'
  import MetricsOverviewCard from './MetricsOverviewCard.svelte'
  import PeakUsageChart from './PeakUsageChart.svelte'
  
  let filters: MetricsFilters = {
    provider_id: '',
    model: '',
    time_range: 'hour'
  }
  
  let overview: MetricsOverview | null = null
  let rpmData: TimeSeriesPoint[] = []
  let tpmInputData: TimeSeriesPoint[] = []
  let rpdData: TimeSeriesPoint[] = []
  let loading = true
  let providers: Provider[] = []
  let models: string[] = []
  let errors: string[] = []
  
  let refreshInterval: number
  
  onMount(async () => {
    await loadProviders()
    await loadModels()
    await loadMetrics()
    
    // Auto-refresh every 30 seconds
    refreshInterval = window.setInterval(loadMetrics, 30000)
  })
  
  onDestroy(() => {
    if (refreshInterval) {
      clearInterval(refreshInterval)
    }
  })
  
  async function loadProviders() {
    try {
      const result = await api.providers.list()
      providers = result || []
      errors = errors.filter(e => !e.includes('providers'))
    } catch (err) {
      const msg = `Failed to load providers: ${(err as Error).message}`
      if (!errors.includes(msg)) {
        errors = [...errors, msg]
      }
      providers = []
    }
  }
  
  async function loadModels() {
    try {
      const result = await api.metrics.models()
      models = result || []
      errors = errors.filter(e => !e.includes('models'))
    } catch (err) {
      const msg = `Failed to load models: ${(err as Error).message}`
      if (!errors.includes(msg)) {
        errors = [...errors, msg]
      }
      models = []
    }
  }
  
  async function loadMetrics() {
    try {
      loading = true
      
      // Load overview and time series in parallel
      const [overviewData, rpmSeries, tpmSeries, rpdSeries] = await Promise.all([
        api.metrics.overview(filters),
        api.metrics.timeSeries('requests', filters),
        api.metrics.timeSeries('tokens_input', filters),
        api.metrics.timeSeries('requests', { ...filters, time_range: '1d' }), // RPD uses daily view
      ])
      
      overview = overviewData
      rpmData = rpmSeries || []
      tpmInputData = tpmSeries || []
      rpdData = rpdSeries || []
      errors = errors.filter(e => !e.includes('metrics'))
    } catch (err) {
      const msg = `Failed to load metrics: ${(err as Error).message}`
      if (!errors.includes(msg)) {
        errors = [...errors, msg]
      }
      rpmData = []
      tpmInputData = []
      rpdData = []
    } finally {
      loading = false
    }
  }
  
  function handleFilterChange(event: CustomEvent<MetricsFilters>) {
    filters = event.detail
    loadMetrics()
  }
</script>

<MetricsFilters 
  bind:filters={filters}
  {providers}
  {models}
  on:change={handleFilterChange}
/>

{#if errors.length > 0}
  {#each errors as error}
    <div class="error-msg">{error}</div>
  {/each}
{/if}

<div class="section">
    <div class="section-header">
      <h2>Overview</h2>
      <span class="icon info-icon">info</span>
    </div>
    <div class="overview-grid">
      <MetricsOverviewCard
        title="Total API Requests"
        value={overview?.total_requests ?? null}
        {loading}
        icon="show_chart"
      />
      <MetricsOverviewCard
        title="Total API Errors"
        value={overview?.total_errors ?? null}
        {loading}
        icon="show_chart"
      />
    </div>
  </div>
  
  <div class="section">
    <div class="section-header">
      <h2>Peak usage trends</h2>
    </div>
    <div class="charts-grid">
      <PeakUsageChart
        title="Peak requests per minute (RPM)"
        data={rpmData}
        {loading}
        showFilterIcon={true}
      />
      <PeakUsageChart
        title="Peak input tokens per minute (TPM)"
        data={tpmInputData}
        {loading}
        showFilterIcon={false}
      />
      <PeakUsageChart
        title="Peak requests per day (RPD)"
        data={rpdData}
        {loading}
        showFilterIcon={true}
      />
    </div>
  </div>

<style>
  .section {
    margin-top: 32px;
  }
  .section-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 16px;
  }
  .section-header h2 {
    font-size: 18px;
    font-weight: 600;
    color: var(--color-text);
  }
  .info-icon {
    font-size: 16px;
    color: var(--color-text-soft);
    cursor: help;
  }
  .overview-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 16px;
  }
  .charts-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 16px;
  }
</style>
