<script lang="ts">
  import { squircle } from '../../../lib/squircle'

  // Project checkbox as a widget: a bare box by default; with `label` the
  // text renders inside the button, so text clicks toggle natively and the
  // accessible name comes from content. Bare rows keep owning their own
  // click handling. Visual language matches the legacy input.check: elev
  // box, accent mark scaling in.
  let {
    checked = $bindable(false),
    label,
    id,
    disabled = false,
    ariaLabel = 'Checkbox',
    onchange
  } = $props<{
    checked?: boolean
    /** optional text beside the box; rendered inside the button */
    label?: string
    id?: string
    disabled?: boolean
    ariaLabel?: string
    onchange?: (v: boolean) => void
  }>()

  function toggle(): void {
    if (disabled) return
    const next = !checked
    checked = next
    onchange?.(next)
  }
</script>

<button
  {id}
  type="button"
  role="checkbox"
  aria-checked={checked}
  aria-label={label ?? ariaLabel}
  class="checkbox"
  class:on={checked}
  class:labeled={label !== undefined}
  {disabled}
  onclick={toggle}
  use:squircle={6}
>
  <span class="checkbox-mark" aria-hidden="true"></span>
  {#if label}<span class="checkbox-label">{label}</span>{/if}
</button>

<style>
  .checkbox {
    flex: none;
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
    cursor: pointer;
    display: inline-grid;
    place-content: center;
  }
  .checkbox-mark {
    width: 14px;
    height: 14px;
    border-radius: 4px;
    background: var(--color-accent);
    transform: scale(0);
    transition: transform 80ms ease-out;
  }
  .checkbox.on .checkbox-mark {
    transform: scale(1);
  }
  /* Labeled variant: the text is button content, so it toggles on click. */
  .checkbox.labeled {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    width: auto;
    height: auto;
    user-select: none;
  }
  .checkbox-label {
    font-size: var(--text-base);
    color: var(--color-text);
  }
  /* Press only: the small box needs a deep scale to read. */
  .checkbox:active:not([disabled]) {
    transform: scale(0.85);
  }
  .checkbox:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .checkbox:focus-visible {
    background:
      linear-gradient(var(--elev), var(--elev)),
      var(--elev);
    box-shadow: var(--focus-ring);
  }
</style>
