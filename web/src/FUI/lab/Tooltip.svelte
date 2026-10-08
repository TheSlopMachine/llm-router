<script lang="ts">
// Tooltip: hover/focus wrapper with delayed popup. Props: text, side, delay, children.
  import type { Snippet } from 'svelte';
  import { squircle } from '../core/squircle';
  let { text, side = 'top', delay = 300, children } = $props<{ text: string; side?: 'top'|'bottom'|'left'|'right'; delay?: number; children: Snippet }>();
  let open = $state(false);
  let t: ReturnType<typeof setTimeout> | undefined;
  function show() { clearTimeout(t); t = setTimeout(() => (open = true), delay); }
  function hide() { clearTimeout(t); open = false; }
</script>
<span class="tip" onmouseenter={show} onmouseleave={hide} onfocusin={show} onfocusout={hide}>
  {@render children()}
  {#if open}<span class="pop" data-side={side} role="tooltip" use:squircle>{text}</span>{/if}
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .tip { position: relative; display: inline-flex; }
  .pop { position: absolute; z-index: 10; max-width: var(--fui-space-8); width: max-content; background: var(--fui-color-surface-container-highest); color: var(--fui-color-text); border-radius: var(--fui-radius-sm); padding: var(--fui-space-2) var(--fui-space-3); font-size: var(--fui-text-xs); line-height: var(--fui-leading-base); white-space: normal; }
  .pop[data-side='top'] { bottom: calc(100% + var(--fui-space-2)); left: 50%; transform: translateX(-50%); }
  .pop[data-side='bottom'] { top: calc(100% + var(--fui-space-2)); left: 50%; transform: translateX(-50%); }
  .pop[data-side='left'] { right: calc(100% + var(--fui-space-2)); top: 50%; transform: translateY(-50%); }
  .pop[data-side='right'] { left: calc(100% + var(--fui-space-2)); top: 50%; transform: translateY(-50%); }
</style>
