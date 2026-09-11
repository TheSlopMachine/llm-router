<script lang="ts">
  import { squircle } from '../../lib/squircle'

  let {
    value = $bindable(''),
    options,
    ariaLabel,
    onchange
  } = $props<{
    value: string
    options: Array<{ value: string; label: string }>
    ariaLabel?: string
    onchange?: (value: string) => void
  }>()

  function select(next: string): void {
    value = next
    onchange?.(next)
  }
</script>

<div class="segmented" role="group" aria-label={ariaLabel} use:squircle={12}>
  {#each options as opt}
    <button
      type="button"
      class="seg-btn"
      class:active={value === opt.value}
      aria-pressed={value === opt.value}
      onclick={() => select(opt.value)}
      use:squircle={9}
    >
      {opt.label}
    </button>
  {/each}
</div>

<style>
  .segmented {
    display: inline-flex;
    height: 36px;
    padding: 3px;
    gap: 0;
    background: var(--color-surface-container-highest);
    border: none;
    border-radius: var(--radius-md);
    overflow: hidden;
  }

  .seg-btn {
    display: flex;
    align-items: center;
    padding: 0 14px;
    height: 100%;
    border-radius: 9px;
    border: none;
    background: transparent;
    color: var(--color-text-soft);
    font-size: 14px;
    font-weight: 500;
    cursor: pointer;
    transition:
      background 0.15s,
      color 0.15s;
  }

  .seg-btn.active {
    background: var(--color-surface);
    color: var(--color-text);
  }
</style>
