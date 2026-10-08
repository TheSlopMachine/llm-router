<script lang="ts">
// RateMeter: заполнение лимита. Пропсы: used, limit, resetIn, locale. Тон авто по заполнению.
  import { squircle } from '../core/squircle';
  let { used = 0, limit = 100, resetIn = '', locale = 'en' } = $props<{ used?: number; limit?: number; resetIn?: string; locale?: string }>();
  const frac = $derived(Math.min(1, used / (limit || 1)));
  const tone = $derived(frac >= 0.9 ? 'var(--fui-color-error-text)' : frac >= 0.7 ? 'var(--fui-color-warning-text)' : 'var(--fui-color-accent)');
  const n = (v: number) => new Intl.NumberFormat(locale).format(v);
</script>
<div class="r" use:squircle role="meter" aria-valuenow={used} aria-valuemax={limit}>
  <div class="row"><span>{n(used)} / {n(limit)}</span>{#if resetIn}<span class="soft">{resetIn}</span>{/if}</div>
  <div class="track"><div class="fill" style:width="{frac * 100}%" style:background={tone}></div></div>
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .r { display: flex; flex-direction: column; gap: var(--fui-space-2); background: var(--fui-elev); border-radius: var(--fui-radius-md); padding: var(--fui-space-4); font-size: var(--fui-text-sm); color: var(--fui-color-text); font-family: var(--fui-font-mono); }
  .row { display: flex; justify-content: space-between; gap: var(--fui-space-3); }
  .soft { color: var(--fui-color-text-soft); }
  .track { height: var(--fui-space-3); background: var(--fui-color-surface-container-highest); border-radius: var(--fui-radius-xs); overflow: hidden; }
  .fill { height: 100%; }
  @media (prefers-reduced-motion: no-preference) { .fill { transition: width 0.3s; } }
</style>
