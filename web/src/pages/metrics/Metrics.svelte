<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { MetricsFilters, MetricsOverview, TimeSeriesPoint, Provider } from '$lib/types'
  import { t } from '$lib/i18n.svelte'
  import { VStack, HStack, Text, Grid, Spacer, Button } from '$ui'
  import MetricsFiltersCmp from './components/MetricsFilters.svelte'
  import MetricsOverviewCard from './components/MetricsOverviewCard.svelte'
  import PeakUsageChart from './components/PeakUsageChart.svelte'

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

  function handleFilterChange(f: any) {
    filters = f
    void loadMetrics()
  }
</script>

<VStack gap={6}>
  <VStack gap={1}>
    <Text tag="h1" size="xl" weight="bold">{t('Metrics')}</Text>
    <Text tone="soft" size="sm">{t('Provider API usage statistics and trends.')}</Text>
  </VStack>

  <MetricsFiltersCmp bind:filters {providers} {models} onchange={handleFilterChange} />

  {#if errors.length > 0}
    <VStack gap={2}>
      {#each errors as error}
        <Text tone="danger" size="sm">{error}</Text>
      {/each}
    </VStack>
  {/if}

  <VStack gap={4}>
    <HStack align="center" gap={2}>
      <Text tag="h2" size="md" weight="bold">{t('Overview')}</Text>
      <Button style="text" size="small" icon={{ name: 'info' }} title={t('Overview information')} />
    </HStack>

    <Grid cols={2} gap={5}>
      <MetricsOverviewCard
        title={t('Total API Requests')}
        value={overview?.total_requests ?? null}
        {loading}
        icon="show_chart"
      />
      <MetricsOverviewCard
        title={t('Total API Errors')}
        value={overview?.total_errors ?? null}
        {loading}
        icon="show_chart"
      />
    </Grid>
  </VStack>

  <VStack gap={4}>
    <Text tag="h2" size="md" weight="bold">{t('Peak usage trends')}</Text>
    <Grid cols={3} gap={5}>
      <PeakUsageChart title={t('Peak requests')} data={requestsData} {loading} />
      <PeakUsageChart title={t('Peak input tokens')} data={inputTokensData} {loading} />
      <PeakUsageChart title={t('Peak output tokens')} data={outputTokensData} {loading} />
    </Grid>
  </VStack>
</VStack>
