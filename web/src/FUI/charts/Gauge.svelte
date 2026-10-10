<script lang="ts">
  import { toneFill } from '../core/tones'
// Gauge: полукруг-заливка value в [min,max]. Пропсы: value, min, max, thresholds [{at,tone}], label.
  import { squircle } from '../core/squircle';
  let { value = 0.5, min = 0, max = 1, thresholds = [], label = '' } = $props<{ value?: number; min?: number; max?: number; thresholds?: { at: number; tone: string }[]; label?: string }>();
  const frac = $derived(Math.min(1, Math.max(0, (value - min) / (max - min || 1))));
  const arc = (f: number) => `M 8 56 A 48 48 0 0 1 ${8 + f * 104} ${56 - Math.sin(f * Math.PI) * 48}`;
  let active = $derived([...thresholds].reverse().find((t) => value >= t.at)?.tone ?? 'accent');
</script>
<div class="g" use:squircle role="meter" aria-valuenow={value} aria-valuemin={min} aria-valuemax={max} aria-label={label}>
  <svg viewBox="0 0 120 64" width="120" height="64" aria-hidden="true">
    <path d="M 8 56 A 48 48 0 0 1 112 56" fill="none" stroke="var(--fui-color-surface-container-highest)" stroke-width="10" stroke-linecap="round" />
    <path d={arc(frac)} fill="none" stroke={toneFill(active)} stroke-width="10" stroke-linecap="round" />
  </svg>
  {#if label}<span class="lb">{label}</span>{/if}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .g { display: inline-flex; flex-direction: column; align-items: center; gap: var(--fui-space-2); background: var(--fui-elev); border-radius: var(--fui-radius-md); padding: var(--fui-space-4); }
  .lb { font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); }
</style>
