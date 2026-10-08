<script lang="ts">
// Rating: star picker. Props: value(bind), max, allowHalf, readonly, size, ariaLabel.
  import Icon from '../controls/Icon.svelte';
  let { value = $bindable(0), max = 5, allowHalf = false, readonly = false, size = 'md', ariaLabel = 'Rating' } = $props<{
    value?: number; max?: number; allowHalf?: boolean; readonly?: boolean; size?: 'sm'|'md'|'lg'; ariaLabel?: string;
  }>();
  function pick(i: number, half: boolean) { if (!readonly) value = half && allowHalf ? i - 0.5 : i; }
</script>
<span class="rate" role={readonly ? 'img' : 'radiogroup'} aria-label={ariaLabel}>
  {#each Array(max) as _, i (i)}
    {@const n = i + 1}
    {@const fill = value >= n ? 'full' : value >= n - 0.5 && allowHalf ? 'half' : 'off'}
    <button type="button" class="star {fill}" disabled={readonly} onclick={(e) => { const r = (e.currentTarget as HTMLElement).getBoundingClientRect(); pick(n, allowHalf && e.clientX - r.left < r.width / 2); }} aria-label={`${n}`}>
      <Icon name={fill === 'half' ? 'star_half' : 'star'} {size} filled={fill !== 'off'} tone={fill === 'off' ? 'disabled' : 'warning'} />
    </button>
  {/each}
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  .rate { display: inline-flex; gap: var(--fui-space-1); }
  .star { display: inline-flex; border-radius: var(--fui-radius-xs); }
  .star:disabled { cursor: default; }
  .star:not(:disabled):hover { background: var(--fui-elev); }
  .star:focus-visible { box-shadow: var(--fui-focus-ring); }
</style>
