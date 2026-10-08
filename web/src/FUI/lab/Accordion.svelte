<script lang="ts">
// Accordion: collapsible sections. Props: items[{id,title,body}], multiple, value(bind: open ids).
  import type { Snippet } from 'svelte';
  import Icon from '../controls/Icon.svelte';
  interface Item { id: string; title: string; body: Snippet; }
  let { items, multiple = false, value = $bindable<string[]>([]) } = $props<{ items: Item[]; multiple?: boolean; value?: string[] }>();
  function toggle(id: string) {
    if (value.includes(id)) value = value.filter((v: string) => v !== id);
    else value = multiple ? [...value, id] : [id];
  }
</script>
<div class="acc">
  {#each items as it (it.id)}
    {@const open = value.includes(it.id)}
    <div class="sec">
      <button type="button" class="head" aria-expanded={open} onclick={() => toggle(it.id)}>
        <span class="title">{it.title}</span>
        <span class="chev" class:open><Icon name="expand_more" size="sm" /></span>
      </button>
      <div class="wrap" class:open><div class="body">{@render it.body()}</div></div>
    </div>
  {/each}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  .acc { display: flex; flex-direction: column; gap: var(--fui-space-2); }
  .sec { background: var(--fui-elev); border-radius: var(--fui-radius-md); }
  .head { display: flex; align-items: center; justify-content: space-between; width: 100%; gap: var(--fui-space-3); padding: var(--fui-space-4) var(--fui-space-5); text-align: left; border-radius: var(--fui-radius-md); }
  .head:focus-visible { box-shadow: var(--fui-focus-ring); }
  .title { font-size: var(--fui-text-base); font-weight: var(--fui-weight-medium); color: var(--fui-color-text); }
  .chev { display: inline-flex; transition: transform 0.2s; color: var(--fui-color-text-soft); }
  .chev.open { transform: rotate(180deg); }
  .wrap { display: grid; grid-template-rows: 0fr; transition: grid-template-rows 0.2s ease; }
  .wrap.open { grid-template-rows: 1fr; }
  .body { overflow: hidden; min-height: 0; font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); }
  .wrap.open .body { padding: 0 var(--fui-space-5) var(--fui-space-4); }
  @media (prefers-reduced-motion: reduce) { .wrap, .chev { transition: none; } }
</style>
