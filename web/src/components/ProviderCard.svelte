<script lang="ts">
  import type { Provider, ProviderStats } from '../lib/types'
  import { squircle } from '../lib/squircle'

  let { provider, stats, onClick } = $props<{
    provider: Provider
    stats: ProviderStats | null
    onClick: () => void
  }>()
</script>

<button class="provider-card" onclick={onClick} use:squircle={18}>
  <div class="provider-header">
    {#if provider.icon_url}
      <img src={provider.icon_url} alt={provider.name} class="provider-icon" use:squircle={12} />
    {:else}
      <span class="icon provider-icon-fallback">cloud</span>
    {/if}
    <div class="provider-info">
      <h3>{provider.name}</h3>
      <div class="credential-count">
        <span class="icon">lock</span>
        <span>{stats?.credential_count ?? 0} active</span>
      </div>
    </div>
  </div>
</button>

<style>
  .provider-card {
    background: var(--color-surface-container-high);
    border-radius: var(--radius-lg);
    padding: 20px;
    cursor: pointer;
    text-align: left;
    transition:
      background-color 0.15s ease,
      transform 0.12s ease;
    width: 100%;
    display: flex;
    align-items: center;
    height: auto;
    font-family: inherit;
    font-size: inherit;
    line-height: inherit;
    box-sizing: border-box;
  }

  .provider-card:hover {
    background: var(--color-surface-container-highest);
  }

  .provider-card:active {
    transform: scale(0.97);
  }

  .provider-header {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
  }

  .provider-icon {
    width: 40px;
    height: 40px;
    border-radius: var(--radius-md);
    object-fit: contain;
    flex-shrink: 0;
  }

  .provider-icon-fallback {
    width: 40px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-surface-container-highest);
    border-radius: var(--radius-md);
    font-size: 24px;
    color: var(--color-text-soft);
    flex-shrink: 0;
  }

  .provider-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .provider-info h3 {
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text);
    margin: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .credential-count {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 13px;
    color: var(--color-text-soft);
  }

  .credential-count .icon {
    font-size: 14px;
    flex-shrink: 0;
  }
</style>
