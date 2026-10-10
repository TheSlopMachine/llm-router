<script lang="ts">
  import { toneFill } from '../core/tones'
// Progress: bar | ring | semicircle. Props: value 0..1|null(indeterminate), variant, size, thickness, tone, label snippet.
  import type { Snippet } from 'svelte';
  let { value, variant = 'bar', size = 48, thickness = 6, tone = 'accent', label } = $props<{
    value: number | null; variant?: 'bar' | 'ring' | 'semicircle'; size?: number; thickness?: number;
    tone?: 'accent' | 'success' | 'warning' | 'danger'; label?: Snippet;
  }>();
  const r = $derived((size - thickness) / 2);
  const circ = $derived(2 * Math.PI * r);
  const pct = $derived(value === null ? 0.25 : Math.max(0, Math.min(1, value)));
  const pctTxt = $derived(value === null ? '' : `${Math.round(pct * 100)}%`);
</script>
<span class="pr" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={value === null ? undefined : Math.round(pct * 100)}>
  {#if variant === 'bar'}
    <span class="bar" class:ind={value === null}><span class="barfill" style:width={value === null ? '40%' : pct * 100 + '%'} style:background={toneFill(tone)}></span></span>
  {:else if variant === 'ring'}
    <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} class:ind={value === null}>
      <circle cx={size/2} cy={size/2} {r} fill="none" stroke="var(--fui-elev)" stroke-width={thickness} />
      <circle cx={size/2} cy={size/2} {r} fill="none" stroke={toneFill(tone)} stroke-width={thickness} stroke-linecap="round" stroke-dasharray={circ} stroke-dashoffset={circ * (1 - pct)} transform={`rotate(-90 ${size/2} ${size/2})`} />
    </svg>
  {:else}
    <svg width={size} height={size / 2 + thickness} viewBox={`0 0 ${size} ${size / 2 + thickness}`} class:ind={value === null}>
      <path d={`M ${thickness} ${size/2} A ${r} ${r} 0 0 1 ${size - thickness} ${size/2}`} fill="none" stroke="var(--fui-elev)" stroke-width={thickness} stroke-linecap="round" />
      <path d={`M ${thickness} ${size/2} A ${r} ${r} 0 0 1 ${size - thickness} ${size/2}`} fill="none" stroke={toneFill(tone)} stroke-width={thickness} stroke-linecap="round" stroke-dasharray={Math.PI * r} stroke-dashoffset={Math.PI * r * (1 - pct)} />
    </svg>
  {/if}
  {#if label}{@render label()}{:else if pctTxt}<span class="pct">{pctTxt}</span>{/if}
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .pr { display: inline-flex; align-items: center; gap: var(--fui-space-3); }
  .bar { display: block; width: var(--fui-basis-sm); height: var(--fui-space-3); background: var(--fui-elev); border-radius: var(--fui-radius-full); overflow: hidden; }
  .barfill { display: block; height: 100%; border-radius: var(--fui-radius-full); }
  .ind .barfill, svg.ind { animation: slide var(--fui-dur-skeleton) ease-in-out infinite alternate; }
  @keyframes slide { from { opacity: var(--fui-opacity-disabled); } to { opacity: 1; } }
  .pct { font-family: var(--fui-font-mono); font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); }
  @media (prefers-reduced-motion: reduce) { .ind .barfill, svg.ind { animation: none; } }
</style>
