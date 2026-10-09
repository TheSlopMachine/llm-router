<script lang="ts">
  import { squircleAuto } from '../core/squircle-baked'

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
      use:squircleAuto={{ bake: size === 'xl' ? 'switch-xl' : 'switch' }}
    >
      <span class="switch-thumb" use:squircleAuto={{ bake: size === 'xl' ? 'switch-thumb-xl' : 'switch-thumb' }}></span>
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
      use:squircleAuto={{ bake: size === 'xl' ? 'switch-xl' : 'switch' }}
    >
    <span class="switch-thumb" use:squircleAuto={{ bake: size === 'xl' ? 'switch-thumb-xl' : 'switch-thumb' }}></span>
  </button>
{/if}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  :where(button:disabled) { cursor: not-allowed; }
  .switch-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--fui-space-4);
    font-size: var(--fui-text-base);
    font-weight: 400;
    color: var(--fui-color-text);
    cursor: pointer;
    user-select: none;
  }

  .switch {
    width: var(--fui-switch-w);
    height: var(--fui-switch-h);
    border-radius: var(--fui-switch-pill);
    border: none;
    background: var(--fui-elev);
    position: relative;
    cursor: pointer;
    padding: 0;
    flex-shrink: 0;
    transition: background var(--fui-dur-base);
  }

  .switch.on {
    background: var(--fui-color-switch-on);
  }

  /* The thumb slide is the feedback: no hover/press transform on the track. */
  .switch:hover:not(.on):not(:disabled) {
    background: var(--fui-color-button-container-high);
  }
  .switch:focus-visible {
    outline: none;
    box-shadow: var(--fui-focus-ring);
  }
  .switch.on:focus-visible {
    /* accent ring on an accent track would vanish */
    box-shadow: var(--fui-focus-ring-contrast);
  }
  .switch:disabled {
    opacity: var(--fui-opacity-dim);
    cursor: not-allowed;
  }
  :global(.dark) .switch:disabled {
    opacity: var(--fui-opacity-hover);
  }

  .switch-thumb {
    position: absolute;
    top: var(--fui-switch-knob-offset);
    left: var(--fui-switch-knob-offset);
    width: var(--fui-switch-knob);
    height: var(--fui-switch-knob);
    border-radius: 50%;
    background: var(--fui-switch-knob-bg);
    transition: transform var(--fui-dur-base);
  }

  .switch.on .switch-thumb {
    transform: translateX(var(--fui-switch-knob-travel));
  }

  /* XL is 2x linear scale for hero placement (provider header). */
  .switch.xl {
    width: var(--fui-switch-xl-w);
    height: var(--fui-switch-xl-h);
  }
  .switch.xl .switch-thumb {
    top: var(--fui-switch-xl-offset);
    left: var(--fui-switch-xl-offset);
    width: var(--fui-switch-xl-knob);
    height: var(--fui-switch-xl-knob);
  }
  .switch.xl.on .switch-thumb {
    transform: translateX(var(--fui-switch-xl-travel));
  }
</style>
