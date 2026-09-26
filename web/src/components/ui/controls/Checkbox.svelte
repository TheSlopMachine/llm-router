<script lang="ts">
  import { squircle } from '../../../lib/squircle'

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

<script lang="ts" module>
  let nextAutoId = 0
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
    <span class="checkbox" aria-hidden="true" use:squircle={6}>
      <span class="checkbox-mark" use:squircle={4}></span>
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
    <span class="checkbox" aria-hidden="true" use:squircle={6}>
      <span class="checkbox-mark" use:squircle={4}></span>
    </span>
  </span>
{/if}

<style>
  .checkbox-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
    flex: none;
  }
  .checkbox-wrap.labeled {
    gap: var(--space-3);
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
    opacity: 0;
    margin: 0;
    cursor: pointer;
  }
  .checkbox {
    width: 18px;
    height: 18px;
    margin: 0;
    padding: 0;
    border: none;
    border-radius: 6px;
    /* Doubled elev: a single wash dissolves into stacked surfaces. */
    background:
      linear-gradient(var(--elev), var(--elev)),
      var(--elev);
    display: inline-grid;
    place-content: center;
    flex: none;
  }
  .checkbox-mark {
    width: 14px;
    height: 14px;
    border-radius: 4px;
    background: var(--color-accent);
    transform: scale(0);
    transition: transform 80ms ease-out;
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
    opacity: 0.5;
  }
  .checkbox-input:disabled {
    cursor: not-allowed;
  }
  .checkbox-input:focus-visible + .checkbox {
    background:
      linear-gradient(var(--elev), var(--elev)),
      var(--elev);
    box-shadow: var(--focus-ring);
  }
  .checkbox-label {
    font-size: var(--text-base);
    color: var(--color-text);
  }
</style>
