<script lang="ts">
  import { squircle } from '../../lib/squircle'

  let {
    value = $bindable(''),
    placeholder = 'Search...',
    disabled = false
  } = $props<{
    value?: string
    placeholder?: string
    disabled?: boolean
  }>()

  // The clear button suppresses blur on mousedown, so the click lands while
  // the field is still focused.
  function clear(e: MouseEvent): void {
    e.preventDefault()
    value = ''
  }
</script>

<!-- Composite field: the container wears the field chrome (fill, radius,
     focus ring via :focus-within), the input is naked inside it. Icons are
     plain flex children — nothing is absolutely positioned. -->
<div class="search-field" use:squircle={12}>
  <span class="search-icon" aria-hidden="true">
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
  </span>
  <input type="text" {placeholder} bind:value {disabled} />
  <button
    type="button"
    class="search-clear"
    onmousedown={clear}
    aria-label="Clear search"
    tabindex="-1"
  >
    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
  </button>
</div>

<style>
  .search-field {
    display: flex;
    align-items: center;
    margin-bottom: 16px;
    padding: var(--field-pad-v) var(--field-pad-h);
    border: 1px solid transparent;
    border-radius: var(--ctl-radius);
    background: var(--color-surface-container-highest);
  }

  .search-field:focus-within {
    box-shadow: inset 0 0 0 2px var(--color-accent);
  }

  .search-field input {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    background: transparent;
    /* focus ring lives on the container */
    box-shadow: none;
  }

  .search-field input:focus {
    background: transparent;
    box-shadow: none;
  }

  /* Icons collapse fully (width → 0), not just fade: the text slides over
     smoothly. Margins carry the spacing so no dead gap survives the
     collapsed slot. */
  .search-icon,
  .search-clear {
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: var(--color-text-soft);
    overflow: hidden;
    transition:
      width 0.2s ease,
      margin 0.2s ease,
      opacity 0.15s ease,
      transform 0.15s ease;
    transform: scale(1);
  }

  .search-icon {
    width: 16px;
    margin-right: 8px;
  }

  .search-clear {
    width: 22px;
    height: 22px;
    margin-left: 8px;
    padding: 0;
    border: none;
    border-radius: 7px;
    background: transparent;
    cursor: pointer;
  }

  .search-field:focus-within .search-icon,
  .search-field:not(:focus-within) .search-clear {
    width: 0;
    margin-left: 0;
    margin-right: 0;
    opacity: 0;
    transform: scale(0.8);
  }

  .search-field:not(:focus-within) .search-clear {
    pointer-events: none;
  }

  .search-clear:hover {
    background: var(--color-hover-bg);
    color: var(--color-text);
  }
</style>
