<script lang="ts">
  // Internal: one row of a MenuPanel. A real <button>, but it owns every
  // visual property itself, so it neither inherits from nor fights Button.
  import type { Snippet } from 'svelte'

  let {
    selected = false,
    highlighted = false,
    disabled = false,
    tint,
    role,
    onclick,
    children,
  } = $props<{
    selected?: boolean
    highlighted?: boolean
    disabled?: boolean
    /** CSS colour for the label (destructive actions etc.) */
    tint?: string
    role: 'option' | 'menuitem'
    onclick: () => void
    children: Snippet
  }>()
</script>

<button
  type="button"
  class="item"
  class:selected
  class:highlighted
  style:color={tint}
  {disabled}
  {role}
  aria-selected={role === 'option' ? selected : undefined}
  {onclick}
>
  {@render children()}
</button>

<style>
  .item {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    width: 100%;
    padding: 6px 12px;
    font-size: var(--text-base);
    font-weight: 400;
    line-height: 21px;
    text-align: left;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text);
    cursor: pointer;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  /* Hovered/picked row sits one elevation level above the panel. */
  .item:hover:not(:disabled),
  .item.highlighted,
  .item.selected {
    background: var(--elev);
  }
  .item.selected {
    font-weight: 500;
  }
  .item:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }
  .item:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
