<script lang="ts">
  let {
    checked = $bindable(false),
    label,
    id,
    disabled = false,
    onchange
  } = $props<{
    checked?: boolean
    label?: string
    id?: string
    disabled?: boolean
    onchange?: (v: boolean) => void
  }>()

  function toggle(): void {
    if (disabled) return
    const next = !checked
    checked = next
    onchange?.(next)
  }
</script>

{#if label}
  <label class="switch-row" for={id}>
    <span>{label}</span>
    <button
      {id}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      class="switch"
      class:on={checked}
      {disabled}
      onclick={toggle}
    >
      <span class="switch-thumb"></span>
    </button>
  </label>
{:else}
  <button
    {id}
    type="button"
    role="switch"
    aria-checked={checked}
    aria-label="Toggle"
    class="switch"
    class:on={checked}
    {disabled}
    onclick={toggle}
  >
    <span class="switch-thumb"></span>
  </button>
{/if}

<style>
  .switch-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    font-size: 14px;
    font-weight: 400;
    color: var(--color-text);
    cursor: pointer;
    user-select: none;
  }

  .switch {
    width: 36px;
    height: 20px;
    border-radius: 9999px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface-container-highest);
    position: relative;
    cursor: pointer;
    padding: 0;
    flex-shrink: 0;
    transition:
      background 0.15s,
      border-color 0.15s;
  }

  .switch.on {
    background: var(--color-text);
    border-color: var(--color-text);
  }

  .switch-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--color-surface);
    box-shadow: var(--shadow-xs);
    transition: transform 0.15s;
  }

  .switch.on .switch-thumb {
    transform: translateX(16px);
  }
</style>
