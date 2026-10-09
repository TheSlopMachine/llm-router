<script lang="ts">
  import { squircleAuto } from '../core/squircle-baked'

  // Native checkbox device: the input carries state, keyboard and
  // screen-reader semantics; the span paints the box. With `label` the text
  // renders in the same <label>, so text clicks toggle natively — no
  // handlers, no a11y workarounds. Bare mode (no label) renders siblings
  // with no wrapper, so hosts keep owning their rows.
  let {
    checked = $bindable(false),
    label,
    id,
    disabled = false,
    ariaLabel = 'Checkbox',
    onchange
  } = $props<{
    checked?: boolean
    /** optional text beside the box; text clicks toggle it */
    label?: string
    id?: string
    disabled?: boolean
    ariaLabel?: string
    onchange?: (v: boolean) => void
  }>()

  function handleChange(e: Event): void {
    const next = (e.currentTarget as HTMLInputElement).checked
    checked = next
    onchange?.(next)
  }
</script>

{#if label}
  <label class="checkbox-wrap labeled" for={id}>
    <input
      {id}
      type="checkbox"
      class="checkbox-input"
      checked={checked}
      {disabled}
      onchange={handleChange}
    />
    <span class="checkbox" aria-hidden="true" use:squircleAuto={{ bake: 'checkbox' }}>
      <span class="checkbox-mark" use:squircleAuto={{ bake: 'checkbox-mark' }}></span>
    </span>
    <span class="checkbox-label">{label}</span>
  </label>
{:else}
  <span class="checkbox-wrap">
    <input
      {id}
      type="checkbox"
      class="checkbox-input"
      checked={checked}
      {disabled}
      aria-label={ariaLabel}
      onchange={handleChange}
    />
    <span class="checkbox" aria-hidden="true" use:squircleAuto={{ bake: 'checkbox' }}>
      <span class="checkbox-mark" use:squircleAuto={{ bake: 'checkbox-mark' }}></span>
    </span>
  </span>
{/if}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(input) { font: inherit; color: inherit; }
  .checkbox-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
    flex: none;
  }
  .checkbox-wrap.labeled {
    gap: var(--fui-space-3);
    cursor: pointer;
    user-select: none;
  }
  .checkbox-wrap:has(.checkbox-input:disabled) {
    cursor: not-allowed;
  }
  /* The input is the click/keyboard surface; the span paints the box. */
  .checkbox-input {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    opacity: 0;
    margin: 0;
    cursor: pointer;
  }
  .checkbox {
    width: var(--fui-checkbox-box);
    height: var(--fui-checkbox-box);
    margin: 0;
    padding: 0;
    border: none;
    border-radius: var(--fui-checkbox-radius);
    pointer-events: none;
    /* Doubled elev: a single wash dissolves into stacked surfaces. */
    background:
      linear-gradient(var(--fui-elev), var(--fui-elev)),
      var(--fui-elev);
    display: inline-grid;
    place-content: center;
    flex: none;
  }
  .checkbox-mark {
    width: var(--fui-checkbox-check);
    height: var(--fui-checkbox-check);
    border-radius: var(--fui-checkbox-check-radius);
    pointer-events: none;
    background: var(--fui-color-accent);
    transform: scale(0);
    transition: transform var(--fui-dur-fast) ease-out;
  }
  .checkbox-input:checked + .checkbox .checkbox-mark {
    transform: scale(1);
  }
  /* Press only: the small box needs a deep scale to read. */
  .checkbox-input:active:not([disabled]) + .checkbox {
    transform: scale(0.85);
  }
  .checkbox-input:disabled + .checkbox,
  .checkbox-input:disabled ~ .checkbox-label {
    opacity: var(--fui-opacity-muted);
  }
  .checkbox-input:disabled {
    cursor: not-allowed;
  }
  .checkbox-input:focus-visible + .checkbox {
    background:
      linear-gradient(var(--fui-elev), var(--fui-elev)),
      var(--fui-elev);
    box-shadow: var(--fui-focus-ring);
  }
  .checkbox-label {
    font-size: var(--fui-text-base);
    color: var(--fui-color-text);
  }
</style>
