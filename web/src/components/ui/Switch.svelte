<script lang="ts">
  import { squircle } from '../../lib/squircle'

  let {
    checked = $bindable(false),
    label,
    id,
    disabled = false,
    ariaLabel = 'Toggle',
    size = 'md',
    onchange
  } = $props<{
    checked?: boolean
    label?: string
    id?: string
    disabled?: boolean
    ariaLabel?: string
    size?: 'md' | 'xl'
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
      class:xl={size === 'xl'}
      {disabled}
      onclick={toggle}
      use:squircle
    >
      <span class="switch-thumb" use:squircle></span>
    </button>
  </label>
{:else}
  <button
    {id}
    type="button"
    role="switch"
    aria-checked={checked}
    aria-label={ariaLabel}
      class="switch"
      class:on={checked}
      class:xl={size === 'xl'}
      {disabled}
      onclick={toggle}
      use:squircle
    >
    <span class="switch-thumb" use:squircle></span>
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
    border: none;
    background: var(--color-switch-off);
    position: relative;
    cursor: pointer;
    padding: 0;
    flex-shrink: 0;
    transition: background 0.15s;
  }

  .switch.on {
    background: var(--color-switch-on);
  }

  /* The thumb slide is the feedback; no global press bounce on the track. */
  .switch:active {
    transform: none;
  }

  .switch-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #fff;
    /* drop-shadow follows the squircle clip; box-shadow would be clipped away */
    filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.3));
    transition: transform 0.15s;
  }

  .switch.on .switch-thumb {
    transform: translateX(16px);
  }

  /* XL is 2x linear scale for hero placement (provider header). */
  .switch.xl {
    width: 72px;
    height: 40px;
  }
  .switch.xl .switch-thumb {
    top: 4px;
    left: 4px;
    width: 32px;
    height: 32px;
  }
  .switch.xl.on .switch-thumb {
    transform: translateX(32px);
  }
</style>
