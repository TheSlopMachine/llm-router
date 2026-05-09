<script lang="ts">
  export let title: string
  export let value: number | null
  export let loading: boolean = false
  export let icon: string = 'show_chart'
</script>

<div class="card">
  <div class="card-header-inline">
    <span class="card-title">{title}</span>
    <span class="icon">{icon}</span>
  </div>
  <div class="card-body">
    {#if loading}
      <div class="loading">Loading...</div>
    {:else if value === null || value === 0}
      <div class="empty-state">No data available</div>
    {:else}
      <div class="metric-value">{value.toLocaleString()}</div>
    {/if}
  </div>
</div>

<style>
  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
    padding: 16px;
    min-height: 180px;
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
  .card-body {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 120px;
  }
  .empty-state {
    color: var(--color-text-soft);
    font-style: italic;
    font-size: 14px;
  }
  .loading {
    color: var(--color-text-soft);
    font-size: 14px;
  }
  .metric-value {
    font-size: 36px;
    font-weight: 600;
    color: var(--color-text);
  }
</style>
