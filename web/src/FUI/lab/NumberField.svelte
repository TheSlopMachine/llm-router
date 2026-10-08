<script lang="ts">
// NumberField: input with -/+ steppers. Props: value(bind), min, max, step, unit, precision, ariaLabel.
  import Icon from '../controls/Icon.svelte';
  let { value = $bindable(0), min, max, step = 1, unit = '', precision = 0, ariaLabel = 'Number' } = $props<{
    value?: number; min?: number; max?: number; step?: number; unit?: string; precision?: number; ariaLabel?: string;
  }>();
  const shown = $derived(value.toFixed(precision));
  function clamp(v: number) { return Math.max(min ?? -Infinity, Math.min(max ?? Infinity, v)); }
  function add(d: number) { value = clamp(Math.round((value + d * step) * 1e6) / 1e6); }
  function onkey(e: KeyboardEvent) {
    if (e.key === 'ArrowUp') { e.preventDefault(); add(1); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); add(-1); }
  }
  function oninput(e: Event) {
    const v = parseFloat((e.target as HTMLInputElement).value);
    if (Number.isFinite(v)) value = clamp(v);
  }
</script>
<span class="nf">
  <button type="button" class="btn" onclick={() => add(-1)} aria-label="Decrease"><Icon name="remove" size="sm" /></button>
  <input class="in" type="number" {step} {min} {max} value={shown} aria-label={ariaLabel} {oninput} onkeydown={onkey} onwheel={(e) => { e.preventDefault(); add(e.deltaY < 0 ? 1 : -1); }} />
  {#if unit}<span class="unit">{unit}</span>{/if}
  <button type="button" class="btn" onclick={() => add(1)} aria-label="Increase"><Icon name="add" size="sm" /></button>
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  .nf { display: inline-flex; align-items: center; gap: var(--fui-space-1); background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-1); }
  .btn { display: inline-flex; padding: var(--fui-space-2); border-radius: var(--fui-radius-xs); color: var(--fui-color-text-soft); }
  .btn:hover { background: var(--fui-color-hover-bg); color: var(--fui-color-text); }
  .btn:focus-visible { box-shadow: var(--fui-focus-ring); }
  .in { width: var(--fui-space-8); background: none; border: 0; outline: none; text-align: center; font-family: var(--fui-font-mono); font-size: var(--fui-text-base); color: var(--fui-color-text); }
  .unit { font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); }
</style>
