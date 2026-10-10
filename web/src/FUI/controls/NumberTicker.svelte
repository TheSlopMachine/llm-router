<script lang="ts">
  import { untrack } from 'svelte';
// NumberTicker: докрутка числа (rAF + easing). Пропсы: value, duration, locale, options, prefix, suffix.
  let { value = 0, duration = 600, locale = 'en', options = {}, prefix = '', suffix = '' } = $props<{ value?: number; duration?: number; locale?: string; options?: Intl.NumberFormatOptions; prefix?: string; suffix?: string }>();
  let shown = $state(untrack(() => value)); let raf = 0;
  const fmt = (v: number) => prefix + new Intl.NumberFormat(locale, options).format(v) + suffix;
  $effect(() => {
    const from = shown; const to = value; const t0 = performance.now();
    if (matchMedia('(prefers-reduced-motion: reduce)').matches || duration <= 0) { shown = to; return; }
    cancelAnimationFrame(raf);
    const step = (t: number) => { const k = Math.min(1, (t - t0) / duration); shown = from + (to - from) * (1 - Math.pow(1 - k, 3)); if (k < 1) raf = requestAnimationFrame(step); };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  });
</script>
<span class="n" aria-live="polite">{fmt(shown)}</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .n { font-family: var(--fui-font-mono); font-size: var(--fui-text-md); color: var(--fui-color-text); }
</style>
