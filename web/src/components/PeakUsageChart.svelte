<script lang="ts">
  import type { TimeSeriesPoint } from '../lib/types'

  let { title, data = [], loading = false } = $props<{
    title: string
    data: TimeSeriesPoint[]
    loading?: boolean
  }>()

  let maxValue = $derived(data.length > 0 ? Math.max(...data.map((d: TimeSeriesPoint) => d.value)) : 0)
  let hasData = $derived(data.length > 0 && maxValue > 0)
  let yAxisMax = $derived(hasData ? maxValue : 20)
  let yAxisMid = $derived(Math.floor(yAxisMax / 2))

  let startMs = $derived.by(() => {
    if (data.length === 0) return 0
    return Math.min(...data.map((d: TimeSeriesPoint) => new Date(d.timestamp).getTime()))
  })
  let endMs = $derived.by(() => {
    if (data.length === 0) return 1
    return Math.max(...data.map((d: TimeSeriesPoint) => new Date(d.timestamp).getTime()))
  })
  let rangeMs = $derived(Math.max(endMs - startMs, 1))

  function formatYLabel(value: number): string {
    if (value >= 1000) {
      return (value / 1000).toFixed(0) + 'K'
    }
    return value.toString()
  }

  function xFor(ts: string): number {
    return ((new Date(ts).getTime() - startMs) / rangeMs) * 300
  }

  function yFor(value: number): number {
    if (yAxisMax === 0) return 120
    return 120 - (value / yAxisMax) * 120
  }
</script>

<div class="card">
  <div class="card-header-inline">
    <span class="card-title">{title}</span>
  </div>
  <div class="chart-container">
    {#if loading}
      <div class="empty-state">Loading...</div>
    {:else}
      <div class="chart-wrapper">
        <div class="y-axis-labels">
          <span class="y-label">{formatYLabel(yAxisMax)}</span>
          <span class="y-label">{formatYLabel(yAxisMid)}</span>
          <span class="y-label">0</span>
        </div>
        <div class="chart-area">
          {#if !hasData}
            <svg width="100%" height="100%" viewBox="0 0 300 120" preserveAspectRatio="none">
              <line x1="0" y1="119" x2="300" y2="119" stroke="#e2e3e4" stroke-width="1" />
              <line x1="0" y1="115" x2="0" y2="123" stroke="#e2e3e4" stroke-width="1" />
              <line x1="300" y1="115" x2="300" y2="123" stroke="#e2e3e4" stroke-width="1" />
            </svg>
          {:else}
            <svg width="100%" height="100%" viewBox="0 0 300 120" preserveAspectRatio="none">
              <!-- Grid lines at peak, peak/2, zero -->
              <line x1="0" y1="0" x2="300" y2="0" stroke="#f4f5f5" stroke-width="1" />
              <line x1="0" y1="60" x2="300" y2="60" stroke="#f4f5f5" stroke-width="1" />
              <line x1="0" y1="119" x2="300" y2="119" stroke="#e2e3e4" stroke-width="1" />

              {#if data.length === 1}
                {@const cx = xFor(data[0].timestamp)}
                {@const cy = yFor(data[0].value)}
                <circle cx={cx} cy={cy} r="3" fill="#2483e2" vector-effect="non-scaling-stroke" />
              {:else}
                {@const points = data
                  .map((d: TimeSeriesPoint) => `${xFor(d.timestamp)},${yFor(d.value)}`)
                  .join(' ')}
                <polyline
                  points={points}
                  fill="none"
                  stroke="#2483e2"
                  stroke-width="2"
                  vector-effect="non-scaling-stroke"
                />
              {/if}

              <!-- Axis ticks -->
              <line x1="0" y1="115" x2="0" y2="123" stroke="#e2e3e4" stroke-width="1" />
              <line x1="300" y1="115" x2="300" y2="123" stroke="#e2e3e4" stroke-width="1" />
            </svg>
          {/if}
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .card {
    background: var(--color-surface-container-high);
    border: none;
    border-radius: var(--radius-lg);
    padding: 16px;
    min-height: 220px;
  }
  .card-header-inline {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;
  }
  .card-title {
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text-soft);
  }
  .chart-container {
    min-height: 160px;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .empty-state {
    color: var(--color-text-soft);
    font-style: italic;
    font-size: 14px;
  }
  .chart-wrapper {
    width: 100%;
    height: 140px;
    display: flex;
    gap: 8px;
  }
  .y-axis-labels {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    padding-top: 4px;
    padding-bottom: 4px;
    font-size: 11px;
    color: var(--color-text-soft);
    min-width: 40px;
    text-align: right;
  }
  .chart-area {
    flex: 1;
    position: relative;
  }
</style>
