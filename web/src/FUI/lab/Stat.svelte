<script lang="ts">
// Stat: KPI card. Props: label, value, delta?, invert?, hint?, sparkline snippet.
  import type { Snippet } from 'svelte';
  import Icon from '../controls/Icon.svelte';
  let { label, value, delta, invert = false, hint = '', sparkline } = $props<{
    label: string; value: string; delta?: number; invert?: boolean; hint?: string; sparkline?: Snippet;
  }>();
  const up = $derived((delta ?? 0) > 0);
  const good = $derived(invert ? !up : up);
  const tone = $derived(delta === undefined || delta === 0 ? 'soft' : good ? 'success' : 'danger');
</script>
<div class="stat">
  <span class="lab">{label}</span>
  <span class="val">{value}</span>
  {#if delta !== undefined}
    <span class="delta d-{tone}"><Icon name={up ? 'trending_up' : 'trending_down'} size="sm" /><span>{delta > 0 ? '+' : ''}{delta}%</span></span>
  {/if}
  {#if sparkline}<span class="spark">{@render sparkline()}</span>{/if}
  {#if hint}<span class="hint">{hint}</span>{/if}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .stat { display: flex; flex-direction: column; gap: var(--fui-space-1); background: var(--fui-elev); border-radius: var(--fui-radius-md); padding: var(--fui-space-4) var(--fui-space-5); min-width: 0; }
  .lab { font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); }
  .val { font-size: var(--fui-text-2xl); font-weight: var(--fui-weight-bold); color: var(--fui-color-text); line-height: var(--fui-leading-none); }
  .delta { display: inline-flex; align-items: center; gap: var(--fui-space-1); font-size: var(--fui-text-sm); font-weight: var(--fui-weight-medium); }
  .d-success { color: var(--fui-color-success-text); } .d-danger { color: var(--fui-color-error-text); } .d-soft { color: var(--fui-color-text-soft); }
  .spark { display: block; } .hint { font-size: var(--fui-text-xs); color: var(--fui-color-text-disabled); }
</style>
