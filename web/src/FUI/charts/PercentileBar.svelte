<script lang="ts">
// PercentileBar: шкала p50/p95/p99. Пропсы: p50, p95, p99, max, unit, locale.
  import { squircle } from '../core/squircle';
  let { p50 = 0, p95 = 0, p99 = 0, max, unit = '', locale = 'en' } = $props<{ p50?: number; p95?: number; p99?: number; max?: number; unit?: string; locale?: string }>();
  const hi = $derived(max ?? Math.max(p50, p95, p99, 1));
  const f = (v: number) => `${new Intl.NumberFormat(locale).format(v)}${unit ? ' ' + unit : ''}`;
</script>
<div class="p" role="img" aria-label="p50 {f(p50)}, p95 {f(p95)}, p99 {f(p99)}">
  <div class="track" use:squircle>
    <div class="fill f50" style:width="{(p50 / hi) * 100}%"></div>
    <div class="mk m95" style:left="{(p95 / hi) * 100}%"></div>
    <div class="mk m99" style:left="{(p99 / hi) * 100}%"></div>
  </div>
  <div class="lbl"><span>p50 {f(p50)}</span><span>p95 {f(p95)}</span><span>p99 {f(p99)}</span></div>
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .p { display: flex; flex-direction: column; gap: var(--fui-space-2); }
  .track { position: relative; height: var(--fui-space-4); background: var(--fui-elev); border-radius: var(--fui-radius-sm); }
  .fill { position: absolute; inset: 0 auto 0 0; background: var(--fui-color-badge-green-text); border-radius: var(--fui-radius-sm); }
  .mk { position: absolute; top: 0; bottom: 0; width: var(--fui-space-1); background: var(--fui-color-badge-yellow-text); }
  .m99 { background: var(--fui-color-badge-red-text); }
  .lbl { display: flex; gap: var(--fui-space-4); font-size: var(--fui-text-xs); color: var(--fui-color-text-soft); font-family: var(--fui-font-mono); }
</style>
