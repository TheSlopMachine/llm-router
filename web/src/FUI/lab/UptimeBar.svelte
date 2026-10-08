<script lang="ts">
// UptimeBar: N столбиков статуса. Пропсы: days [{date,status,note}], locale. Тултип свой (title + hover-блок).
  let { days = [], locale = 'en' } = $props<{ days?: { date: string; status: 'ok' | 'degraded' | 'down' | 'none'; note?: string }[]; locale?: string }>();
  const C: Record<string, string> = { ok: 'var(--fui-color-badge-green-text)', degraded: 'var(--fui-color-badge-yellow-text)', down: 'var(--fui-color-badge-red-text)', none: 'var(--fui-color-surface-container-highest)' };
  let hot = $state(-1);
  const fmt = (d: string) => { const t = new Date(d); return Number.isNaN(+t) ? d : new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(t); };
</script>
<div class="u" role="img">
  {#each days as d, i}
    <div class="cell" onmouseenter={() => hot = i} onmouseleave={() => hot = -1} title="{fmt(d.date)}: {d.status}{d.note ? ' — ' + d.note : ''}">
      <div class="tick" style:background={C[d.status]}></div>
      {#if hot === i}<div class="tip">{fmt(d.date)} · {d.status}{#if d.note} · {d.note}{/if}</div>{/if}
    </div>
  {/each}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .u { display: flex; gap: var(--fui-space-1); align-items: flex-end; }
  .cell { position: relative; }
  .tick { width: var(--fui-space-3); height: var(--fui-space-6); border-radius: var(--fui-radius-xs); }
  .tip { position: absolute; bottom: calc(100% + var(--fui-space-2)); left: 50%; transform: translateX(-50%); white-space: nowrap; background: var(--fui-color-surface-container-highest); color: var(--fui-color-text); font-size: var(--fui-text-xs); padding: var(--fui-space-1) var(--fui-space-2); border-radius: var(--fui-radius-xs); }
</style>
