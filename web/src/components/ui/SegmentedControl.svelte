<script lang="ts">
  let {
    value = $bindable(''),
    options,
    ariaLabel
  } = $props<{
    value: string
    options: Array<{ value: string; label: string }>
    ariaLabel?: string
  }>()
</script>

<div class="segmented" role="group" aria-label={ariaLabel}>
  {#each options as opt}
    <button
      type="button"
      class="seg-btn"
      class:active={value === opt.value}
      aria-pressed={value === opt.value}
      onclick={() => (value = opt.value)}
    >
      {opt.label}
    </button>
  {/each}
</div>

<style>
  .segmented {
    display: inline-flex;
    padding: 4px;
    gap: 4px;
    background: var(--color-surface-container-highest);
    border: 1px solid var(--color-outline-soft);
    border-radius: 9999px;
  }

  .seg-btn {
    padding: 0 14px;
    height: 28px;
    border-radius: 9999px;
    border: 1px solid transparent;
    background: transparent;
    color: var(--color-text-soft);
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition:
      background 0.15s,
      border-color 0.15s,
      color 0.15s,
      box-shadow 0.15s;
  }

  .seg-btn.active {
    background: var(--color-surface);
    border-color: var(--color-outline-light);
    color: var(--color-text);
    box-shadow: var(--shadow-xs);
  }
</style>
