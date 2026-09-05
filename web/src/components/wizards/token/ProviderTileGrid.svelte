<script lang="ts">
  import type { Provider } from '../../../lib/types'
  import { providerDescription } from '../../../lib/token-helpers'

  let {
    providers,
    selected,
    disabled = false,
    onToggle
  } = $props<{
    providers: Provider[]
    selected: Set<string>
    disabled?: boolean
    onToggle: (id: string) => void
  }>()
</script>

{#if providers.length === 0}
  <div class="muted-placeholder">No providers available.</div>
{:else}
  <div class="provider-grid">
    {#each providers as p}
      {@const isSelected = selected.has(p.id)}
      <button
        type="button"
        class="provider-tile"
        class:selected={isSelected}
        onclick={() => !disabled && onToggle(p.id)}
        {disabled}
        aria-pressed={isSelected}
      >
        <div class="tile-head">
          <span class="tile-name">{p.name}</span>
          {#if isSelected}
            <span class="icon tile-check">check_circle</span>
          {/if}
        </div>
        <span class="tile-desc"
          >{providerDescription(p)} <span class="text-muted">· {p.type}{p.qualifier ? ':' + p.qualifier : ''}</span></span
        >
      </button>
    {/each}
  </div>
{/if}

<style>
  .provider-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 12px;
    margin-top: 8px;
  }

  .provider-tile {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 12px 14px;
    border-radius: 12px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface);
    cursor: pointer;
    text-align: left;
    transition:
      border-color 0.15s,
      background 0.15s;
    height: auto;
    width: 100%;
  }

  .provider-tile:hover:not(:disabled) {
    border-color: var(--color-outline-variant);
    background: var(--color-hover-bg);
  }

  .provider-tile.selected {
    border-color: var(--color-text-soft);
    background: var(--color-hover-bg);
  }

  .tile-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    gap: 8px;
  }

  .tile-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tile-check {
    font-size: 18px;
    color: var(--color-text);
    flex-shrink: 0;
  }

  .tile-desc {
    font-size: 12px;
    color: var(--color-text-soft);
    line-height: 16px;
  }
</style>
