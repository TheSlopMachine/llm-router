<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import type { MetricsFilters, MetricsOverview, TimeSeriesPoint, Provider } from '../lib/types'
  import MetricsFiltersCmp from './MetricsFilters.svelte'
  import MetricsOverviewCard from './MetricsOverviewCard.svelte'
  import PeakUsageChart from './PeakUsageChart.svelte'

  let filters = $state<MetricsFilters & { provider_id: string; model: string; time_range: string }>({
    provider_id: '',
    model: '',
    time_range: 'hour'
  } as unknown as MetricsFilters & { provider_id: string; model: string; time_range: string })

  let overview = $state<MetricsOverview | null>(null)
  let requestsData = $state<TimeSeriesPoint[]>([])
  let inputTokensData = $state<TimeSeriesPoint[]>([])
  let outputTokensData = $state<TimeSeriesPoint[]>([])
  let loading = $state(true)
  let providers = $state<Provider[]>([])
  let models = $state<string[]>([])
  let errors = $state<string[]>([])

  let refreshInterval: number | undefined

  onMount(() => {
    void (async () => {
      await loadProviders()
      await loadModels()
      await loadMetrics()
    })()

    refreshInterval = window.setInterval(loadMetrics, 30000)

    return () => {
      if (refreshInterval !== undefined) clearInterval(refreshInterval)
    }
  })

  async function loadProviders() {
    try {
      const result = await api.providers.list()
      providers = result || []
      errors = errors.filter((e) => !e.includes('providers'))
    } catch (err) {
      const msg = `Failed to load providers: ${getErrorMessage(err)}`
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
      errors = errors.filter((e) => !e.includes('models'))
    } catch (err) {
      const msg = `Failed to load models: ${getErrorMessage(err)}`
      if (!errors.includes(msg)) {
        errors = [...errors, msg]
      }
      models = []
    }
  }

  async function loadMetrics() {
    try {
      loading = true

      const [overviewData, reqSeries, inSeries, outSeries] = await Promise.all([
        api.metrics.overview(filters),
        api.metrics.timeSeries('peak_requests', filters),
        api.metrics.timeSeries('peak_input_tokens', filters),
        api.metrics.timeSeries('peak_output_tokens', filters)
      ])

      overview = overviewData
      requestsData = reqSeries || []
      inputTokensData = inSeries || []
      outputTokensData = outSeries || []
      errors = errors.filter((e) => !e.includes('metrics'))
    } catch (err) {
      const msg = `Failed to load metrics: ${getErrorMessage(err)}`
      if (!errors.includes(msg)) {
        errors = [...errors, msg]
      }
      requestsData = []
      inputTokensData = []
      outputTokensData = []
    } finally {
      loading = false
    }
  }

  function handleFilterChange(f: MetricsFilters) {
    filters = f as typeof filters
    void loadMetrics()
  }
</script>

<MetricsFiltersCmp bind:filters {providers} {models} onchange={handleFilterChange} />

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
    <PeakUsageChart title="Peak requests" data={requestsData} {loading} />
    <PeakUsageChart title="Peak input tokens" data={inputTokensData} {loading} />
    <PeakUsageChart title="Peak output tokens" data={outputTokensData} {loading} />
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
