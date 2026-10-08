<script lang="ts">
// RangeSlider: two handles. Props: value [lo,hi](bind), min, max, step, minGap, ariaLabel.
  let { value = $bindable([20, 80] as [number, number]), min = 0, max = 100, step = 1, minGap = 0, ariaLabel = 'Range' } = $props<{
    value?: [number, number]; min?: number; max?: number; step?: number; minGap?: number; ariaLabel?: string;
  }>();
  const lo = $derived(((value[0] - min) / (max - min)) * 100);
  const hi = $derived(((value[1] - min) / (max - min)) * 100);
  function key(which: 0 | 1, e: KeyboardEvent) {
    let v = value[which];
    if (e.key === 'ArrowRight' || e.key === 'ArrowUp') v += step;
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') v -= step;
    else if (e.key === 'Home') v = which === 0 ? min : value[0] + minGap;
    else if (e.key === 'End') v = which === 1 ? max : value[1] - minGap;
    else return;
    e.preventDefault();
    v = Math.max(min, Math.min(max, v));
    value = which === 0 ? [Math.min(v, value[1] - minGap), value[1]] : [value[0], Math.max(v, value[0] + minGap)];
  }
</script>
<span class="rs" role="group" aria-label={ariaLabel}>
  <span class="track">
    <span class="fill" style:left={lo + '%'} style:width={(hi - lo) + '%'}></span>
    <span class="knob" style:left={lo + '%'} role="slider" tabindex="0" aria-valuemin={min} aria-valuemax={value[1] - minGap} aria-valuenow={value[0]} onkeydown={(e) => key(0, e)}></span>
    <span class="knob" style:left={hi + '%'} role="slider" tabindex="0" aria-valuemin={value[0] + minGap} aria-valuemax={max} aria-valuenow={value[1]} onkeydown={(e) => key(1, e)}></span>
  </span>
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .rs { display: inline-flex; min-width: var(--fui-space-8); }
  .track { position: relative; flex: 1; height: var(--fui-space-2); background: var(--fui-elev); border-radius: 9999px; }
  .fill { position: absolute; top: 0; bottom: 0; background: var(--fui-color-accent); border-radius: 9999px; }
  .knob { position: absolute; top: 50%; width: var(--fui-text-md); height: var(--fui-text-md); border-radius: 9999px; background: var(--fui-color-text-on-button-reverse); transform: translate(-50%, -50%); }
  .knob:focus-visible { box-shadow: var(--fui-focus-ring); }
</style>
