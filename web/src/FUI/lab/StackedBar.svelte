<script lang="ts">
// StackedBar: горизонтальная полоса. Пропсы: items [{label,value,tone}], height, showLegend, showPercent, locale.
  import { squircle } from '../core/squircle';
  let { items = [], height = 12, showLegend = true, showPercent = false, locale = 'en' } = $props<{ items?: { label: string; value: number; tone?: string }[]; height?: number; showLegend?: boolean; showPercent?: boolean; locale?: string }>();
  const TONE: Record<string, string> = { b: 'var(--fui-color-badge-blue-bg)', g: 'var(--fui-color-badge-green-bg)', r: 'var(--fui-color-badge-red-bg)', y: 'var(--fui-color-badge-yellow-bg)', p: 'var(--fui-color-badge-purple-bg)', t: 'var(--fui-color-badge-teal-bg)', o: 'var(--fui-color-badge-orange-bg)' };
  const TT: Record<string, string> = { b: 'var(--fui-color-badge-blue-text)', g: 'var(--fui-color-badge-green-text)', r: 'var(--fui-color-badge-red-text)', y: 'var(--fui-color-badge-yellow-text)', p: 'var(--fui-color-badge-purple-text)', t: 'var(--fui-color-badge-teal-text)', o: 'var(--fui-color-badge-orange-text)' };
  const total = $derived(items.reduce((s: number, i: { value: number }) => s + i.value, 0) || 1);
  const pct = (v: number) => new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 }).format(v / total);
</script>
<div class="s">
  <div class="bar" use:squircle style:height="{height}px" role="img">
    {#each items as it}<div class="seg" style:width="{(it.value / total) * 100}%" style:background={TONE[it.tone ?? 'b']} style:color={TT[it.tone ?? 'b']} title="{it.label}: {pct(it.value)}"></div>{/each}
  </div>
  {#if showLegend}<ul class="lg">{#each items as it}<li><span class="sw" style:background={TONE[it.tone ?? 'b']}></span>{it.label}{#if showPercent} {pct(it.value)}{/if}</li>{/each}</ul>{/if}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .s { display: flex; flex-direction: column; gap: var(--fui-space-3); }
  .bar { display: flex; background: var(--fui-elev); border-radius: var(--fui-radius-sm); overflow: hidden; }
  .seg { min-width: 2px; }
  .lg { list-style: none; display: flex; gap: var(--fui-space-4); font-size: var(--fui-text-sm); color: var(--fui-color-text); }
  .sw { display: inline-block; width: var(--fui-space-3); height: var(--fui-space-3); border-radius: var(--fui-radius-xs); margin-right: var(--fui-space-1); }
  li { display: flex; align-items: center; }
</style>
