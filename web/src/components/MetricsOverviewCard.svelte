<script lang="ts">
  import { t } from '../lib/i18n.svelte'
  let { title, value, loading = false, icon = 'show_chart' } = $props<{
    title: string
    value: number | null
    loading?: boolean
    icon?: string
  }>()
</script>

<div class="card">
  <div class="card-header-inline">
    <span class="card-title">{title}</span>
    <span class="icon">{icon}</span>
  </div>
  <div class="card-body">
    {#if loading}
      <div class="loading">{t('Loading...')}</div>
    {:else if value === null || value === 0}
      <div class="empty-state">{t('No data available')}</div>
    {:else}
      <div class="metric-value">{value.toLocaleString()}</div>
    {/if}
  </div>
</div>

<style>
  .card {
    background: var(--color-surface-container-high);
    border: none;
    border-radius: var(--radius-lg);
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
