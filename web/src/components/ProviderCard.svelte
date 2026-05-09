<script lang="ts">
  import type { Provider, ProviderStats } from '../lib/types'

  export let provider: Provider
  export let stats: ProviderStats | null
  export let onClick: () => void
</script>

<button class="provider-card" on:click={onClick}>
  <div class="provider-header">
    {#if provider.icon_url}
      <img src={provider.icon_url} alt={provider.name} class="provider-icon" />
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
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 16px;
    padding: 20px;
    cursor: pointer;
    text-align: left;
    transition: border-color 0.15s ease;
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
    background: var(--color-hover-bg);
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
    border-radius: 8px;
    object-fit: contain;
    flex-shrink: 0;
  }

  .provider-icon-fallback {
    width: 40px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-surface-container-high);
    border-radius: 8px;
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