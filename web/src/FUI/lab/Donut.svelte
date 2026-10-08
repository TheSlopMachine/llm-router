<script lang="ts">
// Donut: кольцо + легенда. Пропсы: items [{label,value,tone}], thickness, gap, centerLabel.
  import type { Snippet } from 'svelte';
  let { items = [], thickness = 14, gap = 2, centerLabel, locale = 'en' } = $props<{ items?: { label: string; value: number; tone?: string }[]; thickness?: number; gap?: number; centerLabel?: Snippet; locale?: string }>();
  const TONE: Record<string, string> = { b: 'var(--fui-color-badge-blue-text)', g: 'var(--fui-color-badge-green-text)', r: 'var(--fui-color-badge-red-text)', y: 'var(--fui-color-badge-yellow-text)', p: 'var(--fui-color-badge-purple-text)', t: 'var(--fui-color-badge-teal-text)', o: 'var(--fui-color-badge-orange-text)', accent: 'var(--fui-color-accent)' };
  const total = $derived(items.reduce((s: number, i: { value: number }) => s + i.value, 0) || 1);
  const R = 44; const C = 2 * Math.PI * R;
  let hot = $state(-1);
  const segs = $derived.by(() => {
    let off = 0;
    return items.map((it: { label: string; value: number; tone?: string }, i: number) => {
      const f = it.value / total;
      const seg = { ...it, f, off, i };
      off += f;
      return seg;
    });
  });
  const fmt = (v: number) => new Intl.NumberFormat(locale).format(v);
</script>
<div class="d">
  <svg viewBox="0 0 100 100" width="100" height="100" role="img">
    {#each segs as s}
      <circle cx="50" cy="50" r={R} fill="none" stroke={TONE[s.tone ?? 'accent'] ?? TONE.accent} stroke-width={hot === s.i ? thickness + 3 : thickness} stroke-dasharray="{Math.max(0, s.f * C - gap)} {C}" stroke-dashoffset={-s.off * C + C / 4} opacity={hot < 0 || hot === s.i ? 1 : 0.4} onmouseenter={() => hot = s.i} onmouseleave={() => hot = -1}><title>{s.label}: {fmt(s.value)}</title></circle>
    {/each}
    {#if centerLabel}<foreignObject x="25" y="35" width="50" height="30">{@render centerLabel()}</foreignObject>{/if}
  </svg>
  <ul class="lg">{#each items as it, i}<li onmouseenter={() => hot = i} onmouseleave={() => hot = -1}><span class="sw" style:background={TONE[it.tone ?? 'accent']}></span>{it.label}</li>{/each}</ul>
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .d { display: flex; align-items: center; gap: var(--fui-space-4); }
  .lg { list-style: none; font-size: var(--fui-text-sm); color: var(--fui-color-text); display: flex; flex-direction: column; gap: var(--fui-space-2); }
  .sw { display: inline-block; width: var(--fui-space-4); height: var(--fui-space-4); border-radius: var(--fui-radius-xs); margin-right: var(--fui-space-2); }
  li { display: flex; align-items: center; }
</style>
