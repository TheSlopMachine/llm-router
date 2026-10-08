<script lang="ts">
// Badge: corner indicator over children. Props: value?, dot, tone, max, position, children.
  import type { Snippet } from 'svelte';
  let { value, dot = false, tone = 'red', max = 99, position = 'tr', children } = $props<{
    value?: number | string; dot?: boolean; tone?: 'red'|'blue'|'green'|'yellow'|'purple'|'teal'|'orange';
    max?: number; position?: 'tr'|'tl'|'br'|'bl'; children: Snippet;
  }>();
  const label = $derived(dot ? '' : typeof value === 'number' && value > max ? `${max}+` : `${value ?? ''}`);
</script>
<span class="wrap">
  {@render children()}
  {#if dot || label !== ''}<span class="badge" data-tone={tone} data-pos={position} class:dot>{label}</span>{/if}
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .wrap { position: relative; display: inline-flex; }
  .badge { position: absolute; display: inline-flex; align-items: center; justify-content: center; min-width: var(--fui-text-md); height: var(--fui-text-md); padding: 0 var(--fui-space-1); border-radius: 9999px; font-size: var(--fui-text-xs); font-weight: var(--fui-weight-medium); }
  .badge[data-pos='tr'] { top: 0; right: 0; transform: translate(40%, -40%); }
  .badge[data-pos='tl'] { top: 0; left: 0; transform: translate(-40%, -40%); }
  .badge[data-pos='br'] { bottom: 0; right: 0; transform: translate(40%, 40%); }
  .badge[data-pos='bl'] { bottom: 0; left: 0; transform: translate(-40%, 40%); }
  .badge.dot { min-width: var(--fui-space-3); width: var(--fui-space-3); height: var(--fui-space-3); padding: 0; }
  .badge[data-tone='red'] { background: var(--fui-color-badge-red-bg); color: var(--fui-color-badge-red-text); }
  .badge[data-tone='blue'] { background: var(--fui-color-badge-blue-bg); color: var(--fui-color-badge-blue-text); }
  .badge[data-tone='green'] { background: var(--fui-color-badge-green-bg); color: var(--fui-color-badge-green-text); }
  .badge[data-tone='yellow'] { background: var(--fui-color-badge-yellow-bg); color: var(--fui-color-badge-yellow-text); }
  .badge[data-tone='purple'] { background: var(--fui-color-badge-purple-bg); color: var(--fui-color-badge-purple-text); }
  .badge[data-tone='teal'] { background: var(--fui-color-badge-teal-bg); color: var(--fui-color-badge-teal-text); }
  .badge[data-tone='orange'] { background: var(--fui-color-badge-orange-bg); color: var(--fui-color-badge-orange-text); }
</style>
