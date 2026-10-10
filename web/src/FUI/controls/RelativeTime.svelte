<script lang="ts">
// RelativeTime: "5 мин назад", автообновление. Пропсы: date, locale, numeric, updateEvery.
  import { onMount, onDestroy } from 'svelte';
  let { date, locale = 'en', numeric = 'auto', updateEvery } = $props<{ date: Date | number | string; locale?: string; numeric?: 'auto' | 'always'; updateEvery?: number }>();
  const ts = $derived(date instanceof Date ? +date : typeof date === 'number' ? date : Date.parse(date));
  let now = $state(Date.now()); let timer: number | undefined;
  function pick(diff: number): { v: number; u: Intl.RelativeTimeFormatUnit } {
    const s = Math.round(diff / 1000);
    if (Math.abs(s) < 60) return { v: s, u: 'second' };
    const m = Math.round(s / 60); if (Math.abs(m) < 60) return { v: m, u: 'minute' };
    const h = Math.round(m / 60); if (Math.abs(h) < 24) return { v: h, u: 'hour' };
    const d = Math.round(h / 24); if (Math.abs(d) < 30) return { v: d, u: 'day' };
    const mo = Math.round(d / 30); if (Math.abs(mo) < 12) return { v: mo, u: 'month' };
    return { v: Math.round(mo / 12), u: 'year' };
  }
  const text = $derived.by(() => { const { v, u } = pick(ts - now); return new Intl.RelativeTimeFormat(locale, { numeric }).format(v, u); });
  const gap = $derived(updateEvery ?? (Math.abs(ts - now) < 36e5 ? 30_000 : 300_000));
  onMount(() => { timer = window.setInterval(() => (now = Date.now()), gap); });
  onDestroy(() => clearInterval(timer));
</script>
<time class="t" datetime={new Date(ts).toISOString()} title={new Date(ts).toLocaleString(locale)}>{text}</time>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .t { font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); }
</style>
