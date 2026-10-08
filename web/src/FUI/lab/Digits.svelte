<script lang="ts">
// Digits: семисегментные цифры на SVG. Пропсы: value (0-9 : . -), size, tone, offOpacity.
  let { value = '', size = 24, tone = 'accent', offOpacity = 0.12 } = $props<{ value?: string; size?: number; tone?: string; offOpacity?: number }>();
  const SEG: Record<string, number[]> = { '0': [1,1,1,1,1,1,0], '1': [0,1,1,0,0,0,0], '2': [1,1,0,1,1,0,1], '3': [1,1,1,1,0,0,1], '4': [0,1,1,0,0,1,1], '5': [1,0,1,1,0,1,1], '6': [1,0,1,1,1,1,1], '7': [1,1,1,0,0,0,0], '8': [1,1,1,1,1,1,1], '9': [1,1,1,1,0,1,1], '-': [0,0,0,0,0,0,1], ' ': [0,0,0,0,0,0,0] };
  // Порядок сегментов: a,b,c,d,e,f,g. Координаты полигонов в боксе 20x36.
  const P = ['M4 2 L16 2 L18 4 L16 6 L4 6 L2 4 Z', 'M18 5 L20 7 L20 16 L18 18 L16 16 L16 7 Z', 'M18 19 L20 21 L20 30 L18 32 L16 30 L16 21 Z', 'M4 30 L16 30 L18 32 L16 34 L4 34 L2 32 Z', 'M2 19 L4 21 L4 30 L2 32 L0 30 L0 21 Z', 'M2 5 L4 7 L4 16 L2 18 L0 16 L0 7 Z', 'M4 16 L16 16 L18 18 L16 20 L4 20 L2 18 Z'];
  const TONE: Record<string, string> = { accent: 'var(--fui-color-accent)', ok: 'var(--fui-color-success-text)', err: 'var(--fui-color-error-text)', warn: 'var(--fui-color-warning-text)' };
  const c = $derived(TONE[tone] ?? TONE.accent);
  const w = $derived(size * 0.6);
</script>
<div class="dg" role="img" aria-label={value}>
  {#each value.split('') as ch}
    {#if ch === '.' || ch === ':'}<span class="dot" style:width="{w * 0.3}px" style:height="{size}px" style:background={c}></span>
    {:else}
      <svg width={w} height={size} viewBox="0 0 20 36" aria-hidden="true">
        {#each P as d, i}<polygon points={d} fill={c} opacity={(SEG[ch] ?? SEG[' '])[i] ? 1 : offOpacity} />{/each}
      </svg>
    {/if}
  {/each}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .dg { display: inline-flex; align-items: center; gap: var(--fui-space-1); background: var(--fui-elev); padding: var(--fui-space-2); border-radius: var(--fui-radius-sm); }
  .dot { display: inline-block; align-self: flex-end; height: var(--fui-space-2); border-radius: var(--fui-radius-xs); margin-bottom: var(--fui-space-1); }
</style>
