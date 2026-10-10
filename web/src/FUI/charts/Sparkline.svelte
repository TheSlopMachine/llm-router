<script lang="ts">
  import { toneFill } from '../core/tones'
// Sparkline: мини-график values. Пропсы: values, variant 'line'|'area'|'bars', width, height, tone, highlightLast, min, max, locale.
  let { values = [], variant = 'line', width = 120, height = 32, tone = 'accent', highlightLast = false, min, max, locale = 'en', label = '' } = $props<{ values?: number[]; variant?: 'line' | 'area' | 'bars'; width?: number; height?: number; tone?: string; highlightLast?: boolean; min?: number; max?: number; locale?: string; label?: string }>();
  const lo = $derived(min ?? Math.min(...(values.length ? values : [0])));
  const hi = $derived(max ?? Math.max(...(values.length ? values : [1])));
  const span = $derived(hi - lo || 1);
  const pts = $derived(values.map((v: number, i: number) => `${((i / Math.max(1, values.length - 1)) * width).toFixed(1)},${(height - ((v - lo) / span) * (height - 4) - 2).toFixed(1)}`).join(' '));
  let hover = $state(-1);
  const c = $derived(toneFill(tone));
  const fmt = (v: number) => new Intl.NumberFormat(locale).format(v);
</script>
<div class="wrap" role="img" aria-label={label} title={hover >= 0 ? fmt(values[hover]) : undefined}>
  {#if variant === 'bars'}
    <div class="bars" style:width="{width}px" style:height="{height}px">
      {#each values as v, i}
        <div class="bar" class:hot={highlightLast && i === values.length - 1} style:height="{((v - lo) / span) * 100}%" style:background={c} onmouseenter={() => hover = i} onmouseleave={() => hover = -1}></div>
      {/each}
    </div>
  {:else}
    <svg {width} {height} aria-hidden="true">
      {#if variant === 'area'}<polygon points="0,{height} {pts} {width},{height}" fill={c} opacity="0.2" />{/if}
      <polyline points={pts} fill="none" stroke={c} stroke-width="1.5" />
      {#if highlightLast && values.length}<circle cx={width - 1} cy={height - ((values[values.length - 1] - lo) / span) * (height - 4) - 2} r="2.5" fill={c} />{/if}
      {#each values as v, i}<circle role="presentation" cx={(i / Math.max(1, values.length - 1)) * width} cy={height - ((v - lo) / span) * (height - 4) - 2} r="6" fill="transparent" onmouseenter={() => hover = i} onmouseleave={() => hover = -1}><title>{fmt(v)}</title></circle>{/each}
    </svg>
  {/if}
  {#if hover >= 0}<span class="tip">{fmt(values[hover])}</span>{/if}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .wrap { position: relative; display: inline-block; }
  .bars { display: flex; align-items: flex-end; gap: var(--fui-space-1); }
  .bar { flex: 1; min-width: var(--fui-sliver); border-radius: var(--fui-radius-xs); }
  .bar.hot { background: var(--fui-color-accent-hover); }
  .tip { position: absolute; top: calc(-1 * var(--fui-space-6)); left: 0; background: var(--fui-color-surface-container-highest); color: var(--fui-color-text); font-size: var(--fui-text-xs); padding: var(--fui-space-1) var(--fui-space-2); border-radius: var(--fui-radius-xs); font-family: var(--fui-font-mono); }
</style>
