<script lang="ts">
// Slider: single value. Props: value(bind), min, max, step, disabled, showValue, ariaLabel.
  let { value = $bindable(0), min = 0, max = 100, step = 1, disabled = false, showValue = false, ariaLabel = 'Value' } = $props<{
    value?: number; min?: number; max?: number; step?: number; disabled?: boolean; showValue?: boolean; ariaLabel?: string;
  }>();
  const pct = $derived(((value - min) / (max - min)) * 100);
  function onkey(e: KeyboardEvent) {
    if (disabled) return;
    if (e.key === 'ArrowRight' || e.key === 'ArrowUp') { e.preventDefault(); value = Math.min(max, value + step); }
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') { e.preventDefault(); value = Math.max(min, value - step); }
    else if (e.key === 'Home') { e.preventDefault(); value = min; }
    else if (e.key === 'End') { e.preventDefault(); value = max; }
  }
</script>
<span class="sl">
  <span class="track" role="slider" tabindex={disabled ? -1 : 0} aria-valuemin={min} aria-valuemax={max} aria-valuenow={value} aria-label={ariaLabel} aria-disabled={disabled} onkeydown={onkey}>
    <span class="fill" style:width={pct + '%'}></span>
    <span class="knob" style:left={pct + '%'}></span>
  </span>
  {#if showValue}<span class="val">{value}</span>{/if}
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .sl { display: inline-flex; align-items: center; gap: var(--fui-space-3); min-width: var(--fui-space-8); }
  .track { position: relative; flex: 1; height: var(--fui-space-2); background: var(--fui-elev); border-radius: var(--fui-radius-full); }
  .track:focus-visible { box-shadow: var(--fui-focus-ring); }
  .fill { position: absolute; inset: 0 auto 0 0; background: var(--fui-color-accent); border-radius: var(--fui-radius-full); }
  .knob { position: absolute; top: 50%; width: var(--fui-text-md); height: var(--fui-text-md); border-radius: var(--fui-radius-full); background: var(--fui-color-text-on-button-reverse); transform: translate(-50%, -50%); }
  .val { font-family: var(--fui-font-mono); font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); }
</style>
