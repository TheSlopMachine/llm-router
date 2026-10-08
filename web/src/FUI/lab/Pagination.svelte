<script lang="ts">
// Pagination: page numbers with ellipses. Props: page(bind), pageCount, siblings, showEdges, prevLabel, nextLabel.
  import Icon from '../controls/Icon.svelte';
  import { fuiText } from '../core/texts';
  let { page = $bindable(1), pageCount, siblings = 1, showEdges = true, prevLabel = '', nextLabel = '' } = $props<{
    page?: number; pageCount: number; siblings?: number; showEdges?: boolean; prevLabel?: string; nextLabel?: string;
  }>();
  const prev = $derived(prevLabel || fuiText('confirm') || '‹');
  const next = $derived(nextLabel || fuiText('confirm') || '›');
  const nums = $derived.by(() => {
    const out: (number | '…')[] = [];
    const lo = Math.max(showEdges ? 2 : 1, page - siblings), hi = Math.min(pageCount - (showEdges ? 1 : 0), page + siblings);
    if (showEdges) out.push(1);
    if (lo > (showEdges ? 2 : 1)) out.push('…');
    for (let i = lo; i <= hi; i++) if (!out.includes(i)) out.push(i);
    if (hi < pageCount - (showEdges ? 1 : 0)) out.push('…');
    if (showEdges && pageCount > 1 && !out.includes(pageCount)) out.push(pageCount);
    return out;
  });
</script>
<nav class="pg" aria-label="Pagination">
  <button type="button" class="btn" disabled={page <= 1} onclick={() => (page -= 1)} aria-label="Previous"><Icon name="chevron_left" size="sm" /></button>
  {#each nums as n, i (i)}
    {#if n === '…'}<span class="dots" aria-hidden="true">…</span>
    {:else}<button type="button" class="btn" class:on={n === page} aria-current={n === page ? 'page' : undefined} onclick={() => (page = n)}>{n}</button>{/if}
  {/each}
  <button type="button" class="btn" disabled={page >= pageCount} onclick={() => (page += 1)} aria-label="Next"><Icon name="chevron_right" size="sm" /></button>
  <span class="vh">{prev} {next}</span>
</nav>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  .pg { display: flex; align-items: center; gap: var(--fui-space-1); }
  .btn { min-width: var(--fui-ctl-small); height: var(--fui-ctl-small); padding: 0 var(--fui-space-3); display: inline-flex; align-items: center; justify-content: center; border-radius: var(--fui-radius-sm); font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); background: var(--fui-elev); }
  .btn.on { background: var(--fui-color-accent); color: var(--fui-color-text-on-button-reverse); }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn:not(:disabled):not(.on):hover { background: var(--fui-color-hover-bg); color: var(--fui-color-text); }
  .btn:focus-visible { box-shadow: var(--fui-focus-ring); }
  .dots { color: var(--fui-color-text-disabled); padding: 0 var(--fui-space-1); }
  .vh { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
</style>
