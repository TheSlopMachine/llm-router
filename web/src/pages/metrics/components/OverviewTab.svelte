<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { MetricsFilters, MetricsOverview, TimeSeriesPoint, Provider } from '$lib/types'
  import { t } from '$lib/i18n.svelte'
  import { Banner, Button, Card, Grid, HStack, Stat, Text, VStack } from '@fuckyouryan/fui'
  import MetricsFiltersCmp from './MetricsFilters.svelte'
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

  // Live: a refresh every few seconds, skipped while hidden or while the previous one runs.
  const LIVE_INTERVAL = 5000
  let live = $state(true)
  let clock = $state(Date.now())
  let pendingRequests = 0
  let refreshInterval: number | undefined
  let latestMetricsRequest = 0

  onMount(() => {
    void (async () => {
      await loadProviders()
      await loadModels()
      await loadMetrics()
    })()

    refreshInterval = window.setInterval(() => {
      if (live && !document.hidden && pendingRequests === 0) void loadMetrics()
    }, LIVE_INTERVAL)

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
      const message = `Failed to load providers: ${getErrorMessage(err)}`
      if (!errors.includes(message)) {
        errors = [...errors, message]
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
      const message = `Failed to load models: ${getErrorMessage(err)}`
      if (!errors.includes(message)) {
        errors = [...errors, message]
      }
      models = []
    }
  }

  async function loadMetrics() {
    const request = ++latestMetricsRequest
    pendingRequests++
    try {
      // Only the first load shows the loading state; refreshes replace data in place.
      if (overview === null) loading = true

      const [overviewData, reqSeries, inSeries, outSeries] = await Promise.all([
        api.metrics.overview(filters),
        api.metrics.timeSeries('peak_requests', filters),
        api.metrics.timeSeries('peak_input_tokens', filters),
        api.metrics.timeSeries('peak_output_tokens', filters)
      ])
      if (request !== latestMetricsRequest) return

      clock = Date.now()
      overview = overviewData
      requestsData = reqSeries || []
      inputTokensData = inSeries || []
      outputTokensData = outSeries || []
      errors = errors.filter((e) => !e.includes('metrics'))
    } catch (err) {
      if (request !== latestMetricsRequest) return
      const message = `Failed to load metrics: ${getErrorMessage(err)}`
      if (!errors.includes(message)) {
        errors = [...errors, message]
      }
      requestsData = []
      inputTokensData = []
      outputTokensData = []
    } finally {
      pendingRequests--
      if (request === latestMetricsRequest) loading = false
    }
  }

  // Start of the charted range; the end is the last refresh.
  function rangeStart(timeRange: string, now: number): number {
    const day = 86_400_000
    switch (timeRange) {
      case '1d': return now - day
      case '7d': return now - 7 * day
      case '28d': return now - 28 * day
      case '90d': return now - 90 * day
      case 'month': {
        const date = new Date(now)
        return new Date(date.getFullYear(), date.getMonth(), 1).getTime()
      }
      default: return now - 3_600_000
    }
  }
  const chartFrom = $derived(rangeStart(filters.time_range, clock))

  const placeholder = $derived(loading ? t('common.state.loading') : '—')
  const totalRequests = $derived(overview === null ? placeholder : overview.total_requests.toLocaleString())
  const totalErrors = $derived(overview === null ? placeholder : overview.total_errors.toLocaleString())
  const errorRate = $derived(
    overview === null || overview.total_requests === 0
      ? placeholder
      : `${((overview.total_errors / overview.total_requests) * 100).toFixed(2)}%`,
  )
  const errorTint = $derived(overview !== null && overview.total_errors > 0 ? 'danger' : undefined)

  function handleFilterChange(f: any) {
    filters = f
    overview = null
    void loadMetrics()
  }
</script>

<VStack gap={6}>
  <HStack gap={3} valign="end" wrap>
    <MetricsFiltersCmp bind:filters {providers} {models} onchange={handleFilterChange} />
    <Button
      buttonStyle={live ? 'prominent' : 'neutral'}
      icon={{ name: live ? 'sensors' : 'pause' }}
      text={t('metrics.live.action')}
      onclick={() => {
        live = !live
        if (live) void loadMetrics()
      }}
    />
  </HStack>

  {#if errors.length > 0}
    <VStack gap={2}>
      {#each errors as error}
        <Banner tint="danger" text={error} />
      {/each}
    </VStack>
  {/if}

  <VStack gap={4}>
    <Text variant="section-title">{t('metrics.overview')}</Text>
    <Grid min="md" gap={5}>
      <Card>
        <Stat label={t('metrics.total_requests')} value={totalRequests} />
      </Card>
      <Card>
        <Stat label={t('metrics.total_errors')} value={totalErrors} tint={errorTint} />
      </Card>
      <Card>
        <Stat label={t('metrics.error_rate.label')} value={errorRate} tint={errorTint} />
      </Card>
    </Grid>
  </VStack>

  <VStack gap={4}>
    <Text variant="section-title">{t('metrics.peak_trends')}</Text>
    <Grid min="md" gap={5}>
      <PeakUsageChart title={t('metrics.peak_requests')} data={requestsData} from={chartFrom} to={clock} {loading} />
      <PeakUsageChart title={t('metrics.peak_input')} data={inputTokensData} from={chartFrom} to={clock} {loading} />
      <PeakUsageChart title={t('metrics.peak_output')} data={outputTokensData} from={chartFrom} to={clock} {loading} />
    </Grid>
  </VStack>
</VStack>
