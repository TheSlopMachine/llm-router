<script lang="ts">
  import { GroupBox, Histogram, Text } from '@fuckyouryan/fui'
  import { locale, t } from '$lib/i18n.svelte'
  import type { TimeSeriesPoint } from '$lib/types'

  // The time axis is a fixed row of bars: a quiet stretch is an empty bar, not a wider one.
  const BAR_COUNT = 60
  const DAY = 86_400_000

  let { title, data = [], from, to, loading = false }: {
    title: string
    data: TimeSeriesPoint[]
    /** ms, start and end of the charted range */
    from: number
    to: number
    loading?: boolean
  } = $props()

  const formatter = $derived(
    new Intl.DateTimeFormat(locale(), {
      ...(to - from >= DAY ? { month: 'short', day: 'numeric' } : {}),
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    }),
  )

  // A bar shows the peak among the points that fall into its slice of the range.
  const bins = $derived.by(() => {
    const width = Math.max(1, (to - from) / BAR_COUNT)
    const peaks = Array.from({ length: BAR_COUNT }, () => 0)
    for (const point of data) {
      const index = Math.min(BAR_COUNT - 1, Math.max(0, Math.floor((Date.parse(point.timestamp) - from) / width)))
      peaks[index] = Math.max(peaks[index], point.value)
    }
    return peaks.map((peak, index) => ({ label: formatter.format(new Date(from + index * width)), values: [peak] }))
  })
  const hasData = $derived(bins.some((bin) => bin.values[0] > 0))
</script>

<GroupBox {title}>
  {#if loading}
    <Text foregroundStyle="secondary" size="sm">{t('common.state.loading')}</Text>
  {:else if !hasData}
    <Text foregroundStyle="secondary" size="sm">{t('common.empty.no_data')}</Text>
  {:else}
    <Histogram {bins} series={[{ name: title, tint: 'accent' }]} height={120} locale={locale()} label={title} />
  {/if}
</GroupBox>
