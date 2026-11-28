<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { MetricsFilters, MetricsOverview, TimeSeriesPoint, Provider } from '$lib/types'
  import { t, type TranslationKey } from '$lib/i18n.svelte'
  import { Banner, Button, Card, Grid, HStack, Section, Stat, VStack } from '@fuckyouryan/fui'
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
  // One slot per loader: a loader that succeeds clears its own failure only.
  type LoadSource = 'providers' | 'models' | 'activity' | 'metrics'
  const LOAD_ERROR_KEYS: Record<LoadSource, TranslationKey> = {
    providers: 'metrics.load_providers.error',
    models: 'metrics.load_models.error',
    activity: 'metrics.last_activity.error',
    metrics: 'metrics.load_data.error',
  }
  let loadErrors = $state<Record<LoadSource, string>>({ providers: '', models: '', activity: '', metrics: '' })
  function reportLoad(source: LoadSource, failure: unknown = null): void {
    loadErrors[source] = failure === null ? '' : `${t(LOAD_ERROR_KEYS[source])}: ${getErrorMessage(failure)}`
  }

  // Live: a refresh every few seconds, skipped while hidden or while the previous one runs.
  const LIVE_INTERVAL = 5000
  let live = $state(true)
  let clock = $state(Date.now())
  let pendingRequests = 0
  let refreshInterval: number | undefined
  let latestMetricsRequest = 0

  // Ranges in growing order for the activity pick; 'month' is a calendar range, not a step.
  const ACTIVITY_RANGES = ['hour', '1d', '7d', '28d', '90d'] as const
  // A range the user picked stays; until then the range follows the last activity.
  let rangePickedByUser = false

  onMount(() => {
    void (async () => {
      await loadProviders()
      await loadModels()
      await pickRangeFromActivity()
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
      reportLoad('providers')
    } catch (err) {
      reportLoad('providers', err)
      providers = []
    }
  }

  async function loadModels() {
    try {
      const result = await api.metrics.models()
      models = result || []
      reportLoad('models')
    } catch (err) {
      reportLoad('models', err)
      models = []
    }
  }

  // Selects the shortest range holding the last request; no activity selects the shortest range.
  async function pickRangeFromActivity() {
    if (rangePickedByUser) return
    let lastActivity: string | null
    try {
      lastActivity = await api.metrics.lastActivity(filters)
      reportLoad('activity')
    } catch (err) {
      reportLoad('activity', err)
      return
    }
    if (rangePickedByUser) return
    const now = Date.now()
    const activityTime = lastActivity === null ? null : Date.parse(lastActivity)
    filters.time_range = activityTime === null
      ? ACTIVITY_RANGES[0]
      : ACTIVITY_RANGES.find((range) => rangeStart(range, now) <= activityTime) ?? ACTIVITY_RANGES[ACTIVITY_RANGES.length - 1]
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
      reportLoad('metrics')
    } catch (err) {
      if (request !== latestMetricsRequest) return
      reportLoad('metrics', err)
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

  async function handleFilterChange(changed: typeof filters, field: 'provider_id' | 'time_range' | 'model') {
    filters = changed
    overview = null
    if (field === 'time_range') rangePickedByUser = true
    else await pickRangeFromActivity()
    await loadMetrics()
  }
</script>

{#if Object.values(loadErrors).some(Boolean)}
  <VStack>
    {#each Object.entries(loadErrors) as [source, message] (source)}
      {#if message}<Banner tint="danger" text={message} />{/if}
    {/each}
  </VStack>
{/if}

<Section title={t('metrics.overview')}>
  <Grid min="md" spacing="group">
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
</Section>

<HStack spacing="group" valign="end" wrap>
  <VStack grow>
    <MetricsFiltersCmp bind:filters {providers} {models} onchange={(changed, field) => void handleFilterChange(changed, field)} />
  </VStack>
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

<Section title={t('metrics.peak_trends')}>
  <Grid min="md" spacing="group">
    <PeakUsageChart title={t('metrics.peak_requests')} data={requestsData} from={chartFrom} to={clock} {loading} />
    <PeakUsageChart title={t('metrics.peak_input')} data={inputTokensData} from={chartFrom} to={clock} {loading} />
    <PeakUsageChart title={t('metrics.peak_output')} data={outputTokensData} from={chartFrom} to={clock} {loading} />
  </Grid>
</Section>
