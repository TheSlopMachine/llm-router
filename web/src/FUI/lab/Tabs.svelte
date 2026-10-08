<script lang="ts">
// Tabs: pill indicator slides under active tab. Props: items[{id,label,icon?,badge?}], value(bind), size, fullWidth.
  import Icon from '../controls/Icon.svelte';
  import { squircle } from '../core/squircle';
  interface Item { id: string; label: string; icon?: string; badge?: string; }
  let { items, value = $bindable(''), size = 'medium', fullWidth = false } = $props<{
    items: Item[]; value?: string; size?: 'small'|'medium'|'large'; fullWidth?: boolean;
  }>();
  function onkey(e: KeyboardEvent) {
    const i = items.findIndex((t: Item) => t.id === value);
    let n = i;
    if (e.key === 'ArrowRight') n = (i + 1) % items.length;
    else if (e.key === 'ArrowLeft') n = (i - 1 + items.length) % items.length;
    else if (e.key === 'Home') n = 0;
    else if (e.key === 'End') n = items.length - 1;
    else return;
    e.preventDefault(); value = items[n].id;
  }
</script>
<div class="tabs" role="tablist" class:full={fullWidth} onkeydown={onkey} use:squircle>
  {#each items as t (t.id)}
    <button type="button" role="tab" aria-selected={t.id === value} class="tab" class:on={t.id === value} data-size={size} onclick={() => (value = t.id)} tabindex={t.id === value ? 0 : -1}>
      {#if t.icon}<Icon name={t.icon} size="sm" />{/if}
      <span>{t.label}</span>
      {#if t.badge}<span class="badge">{t.badge}</span>{/if}
    </button>
  {/each}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  .tabs { display: inline-flex; gap: var(--fui-space-1); background: var(--fui-elev); border-radius: var(--fui-radius-md); padding: var(--fui-space-1); }
  .tabs.full { display: flex; } .tabs.full .tab { flex: 1; }
  .tab { display: inline-flex; align-items: center; gap: var(--fui-space-2); border-radius: var(--fui-radius-sm); padding: var(--fui-space-2) var(--fui-space-4); font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); transition: background 0.15s, color 0.15s; }
  .tab[data-size='small'] { font-size: var(--fui-text-xs); padding: var(--fui-space-1) var(--fui-space-3); }
  .tab[data-size='large'] { font-size: var(--fui-text-md); padding: var(--fui-space-3) var(--fui-space-5); }
  .tab.on { background: var(--fui-color-accent); color: var(--fui-color-text-on-button-reverse); }
  .tab:not(.on):hover { background: var(--fui-color-hover-bg); color: var(--fui-color-text); }
  .tab:focus-visible { box-shadow: var(--fui-focus-ring); }
  .tab.on:focus-visible { box-shadow: var(--fui-focus-ring-contrast); }
  .badge { background: var(--fui-color-accent-soft); border-radius: var(--fui-radius-xs); padding: 0 var(--fui-space-2); font-size: var(--fui-text-xs); }
  .tab.on .badge { background: var(--fui-elev); color: inherit; }
  @media (prefers-reduced-motion: reduce) { .tab { transition: none; } }
</style>
