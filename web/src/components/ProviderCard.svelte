<script lang="ts">
  import type { Provider, ProviderStats } from '../lib/types'
  import { squircle } from '../lib/squircle'
  import Switch from './ui/Switch.svelte'
  import { t, n } from '../lib/i18n.svelte'

  let { provider, stats, onClick, onToggle } = $props<{
    provider: Provider
    stats: ProviderStats | null
    onClick: () => void
    onToggle: (enabled: boolean) => void
  }>()

  function onCardKeydown(e: KeyboardEvent): void {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onClick()
    }
  }
</script>

<!-- The whole card is the click target (no inner button rectangle). The toggle
     wrapper eats its own clicks/keys and extends a ~30px dead zone around the
     switch via padding cancelled by negative margin — layout is unchanged, but
     taps near the switch never trigger the card. -->
<div
  class="provider-card"
  class:disabled={provider.disabled}
  role="button"
  tabindex="0"
  aria-label={provider.name}
  onclick={onClick}
  onkeydown={onCardKeydown}
  use:squircle={18}
>
  <div class="provider-main">
    <div class="provider-header">
      {#if provider.icon_url}
        <img src={provider.icon_url} alt="" class="provider-icon" use:squircle={12} />
      {:else}
        <span class="icon provider-icon-fallback">cloud</span>
      {/if}
      <div class="provider-info">
        <h3>{provider.name}</h3>
        <div class="credential-count">
          <span class="icon">lock</span>
          <span>{n(stats?.credential_count ?? 0, 'active', 'active', 'активен', 'активны', 'активно')}</span>
        </div>
      </div>
    </div>
  </div>
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <span
    class="provider-toggle"
    onclick={(e) => e.stopPropagation()}
    onkeydown={(e) => e.stopPropagation()}
  >
    <Switch
      checked={!provider.disabled}
      ariaLabel={t('Enable provider')}
      onchange={(v) => onToggle(v)}
    />
  </span>
</div>

<style>
  .provider-card {
    background: var(--color-surface-container-high);
    border-radius: var(--radius-lg);
    padding: 20px;
    transition:
      background-color 0.15s ease,
      opacity 0.15s ease;
    width: 100%;
    display: flex;
    align-items: center;
    gap: 12px;
    box-sizing: border-box;
    cursor: pointer;
  }

  .provider-card:hover {
    background: var(--color-surface-container-highest);
  }

  .provider-card:focus-visible {
    /* inset ring: follows the squircle clip, unlike outline */
    box-shadow: inset 0 0 0 2px var(--color-accent);
    outline: none;
  }

  .provider-card.disabled {
    opacity: 0.55;
  }

  .provider-card.disabled:hover {
    background: var(--color-surface-container-high);
  }

  .provider-main {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
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

  /* Dead zone: padding extends the hit area ~30px past the switch on every
     side, negative margin cancels the layout cost. The card clip-path trims
     whatever bleeds outside the card, so only the in-card zone stays inert. */
  .provider-toggle {
    flex-shrink: 0;
    padding: 30px;
    margin: -30px;
  }
</style>
