<script lang="ts">
// DescriptionList: key-value pairs. Props: items[{key,value|body}], columns, orientation.
  import type { Snippet } from 'svelte';
  interface Item { key: string; value?: string; body?: Snippet; }
  let { items, columns = 1, orientation = 'horizontal' } = $props<{ items: Item[]; columns?: number; orientation?: 'horizontal' | 'vertical' }>();
</script>
<dl class="dl" class:vert={orientation === 'vertical'} style:grid-template-columns={`repeat(${columns}, 1fr)`}>
  {#each items as it (it.key)}
    <div class="row">
      <dt>{it.key}</dt>
      <dd>{#if it.body}{@render it.body()}{:else}{it.value}{/if}</dd>
    </div>
  {/each}
</dl>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .dl { display: grid; gap: var(--fui-space-4); }
  .row { display: flex; gap: var(--fui-space-4); min-width: 0; }
  .vert .row { flex-direction: column; gap: var(--fui-space-1); }
  .row dt { font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); flex-shrink: 0; min-width: var(--fui-space-8); }
  .row dd { font-size: var(--fui-text-sm); color: var(--fui-color-text); min-width: 0; }
</style>
