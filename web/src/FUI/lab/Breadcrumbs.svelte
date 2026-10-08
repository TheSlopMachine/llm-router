<script lang="ts">
// Breadcrumbs: trail items, overflow collapses to …. Props: items[{label,href?}], separator, maxItems.
  interface Item { label: string; href?: string; }
  let { items, separator = '/', maxItems = 4, ariaLabel = 'Breadcrumb' } = $props<{ items: Item[]; separator?: string; maxItems?: number; ariaLabel?: string }>();
  const shown = $derived(items.length > maxItems ? [{ label: '…', collapsed: true } as Item & {collapsed?:boolean}, ...items.slice(-(maxItems - 1))] : items);
</script>
<nav class="crumbs" aria-label={ariaLabel}>
  <ol>
    {#each shown as it, i (i)}
      <li>
        {#if it.href}<a href={it.href}>{it.label}</a>{:else}<span aria-current={i === shown.length - 1 ? 'page' : undefined}>{it.label}</span>{/if}
        {#if i < shown.length - 1}<span class="sep" aria-hidden="true">{separator}</span>{/if}
      </li>
    {/each}
  </ol>
</nav>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .crumbs ol { display: flex; flex-wrap: wrap; align-items: center; gap: var(--fui-space-2); list-style: none; }
  .crumbs li { display: inline-flex; align-items: center; gap: var(--fui-space-2); font-size: var(--fui-text-sm); }
  .crumbs a { color: var(--fui-color-text-link); text-decoration: none; }
  .crumbs span[aria-current] { color: var(--fui-color-text); }
  .crumbs li > span:not(.sep) { color: var(--fui-color-text-soft); }
  .sep { color: var(--fui-color-text-disabled); }
</style>
