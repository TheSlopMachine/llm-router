<script lang="ts">
  import type { TimeSeriesPoint } from '../lib/types'
  
  export let title: string
  export let data: TimeSeriesPoint[] = []
  export let loading: boolean = false
  export let showFilterIcon: boolean = false
  
  $: maxValue = data.length > 0 ? Math.max(...data.map(d => d.value)) : 0
  $: hasData = data.length > 0 && maxValue > 0
  
  // Calculate Y-axis scale
  $: yAxisMax = hasData ? calculateYMax(maxValue) : 20
  $: yAxisMid = Math.floor(yAxisMax / 2)
  
  function calculateYMax(max: number): number {
    if (max === 0) return 20
    
    // Round up to nice number
    const magnitude = Math.pow(10, Math.floor(Math.log10(max)))
    const normalized = max / magnitude
    
    let nice: number
    if (normalized <= 1) nice = 1
    else if (normalized <= 2) nice = 2
    else if (normalized <= 5) nice = 5
    else nice = 10
    
    return nice * magnitude * 1.2 // Add 20% headroom
  }
  
  function formatYLabel(value: number): string {
    if (value >= 1000) {
      return (value / 1000).toFixed(0) + 'K'
    }
    return value.toString()
  }
</script>

<div class="card">
  <div class="card-header-inline">
    <span class="card-title">{title}</span>
    {#if showFilterIcon}
      <span class="icon">filter_list</span>
    {/if}
  </div>
  <div class="chart-container">
    {#if loading}
      <div class="empty-state">Loading...</div>
    {:else}
      <div class="chart-wrapper">
        <div class="y-axis-labels">
          <span class="y-label">{formatYLabel(yAxisMax)}</span>
          <span class="y-label">{formatYLabel(yAxisMid)}</span>
        </div>
        <div class="chart-area">
          {#if !hasData}
            <svg width="100%" height="100%" viewBox="0 0 300 120" preserveAspectRatio="none">
              <!-- Empty chart with axes -->
              <line x1="0" y1="119" x2="300" y2="119" stroke="#e2e3e4" stroke-width="1" />
              <line x1="0" y1="115" x2="0" y2="123" stroke="#e2e3e4" stroke-width="1" />
              <line x1="300" y1="115" x2="300" y2="123" stroke="#e2e3e4" stroke-width="1" />
            </svg>
          {:else}
            <svg width="100%" height="100%" viewBox="0 0 300 120" preserveAspectRatio="none">
              <!-- Grid lines -->
              <line x1="0" y1="0" x2="300" y2="0" stroke="#f4f5f5" stroke-width="1" />
              <line x1="0" y1="60" x2="300" y2="60" stroke="#f4f5f5" stroke-width="1" />
              <line x1="0" y1="119" x2="300" y2="119" stroke="#e2e3e4" stroke-width="1" />
              
              <!-- Data line -->
              {#if data.length > 1}
                {@const points = data.map((d, i) => {
                  const x = (i / (data.length - 1)) * 300
                  const y = 120 - ((d.value / yAxisMax) * 120)
                  return `${x},${y}`
                }).join(' ')}
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
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
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
