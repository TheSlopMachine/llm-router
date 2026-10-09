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
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  :where(button:disabled) { cursor: not-allowed; }
  .item {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    width: 100%;
    padding: var(--fui-menu-item-pad-y) var(--fui-menu-item-pad-x);
    font-size: var(--fui-text-base);
    font-weight: 400;
    line-height: var(--fui-menu-item-line);
    text-align: left;
    border: none;
    border-radius: var(--fui-menu-item-radius);
    background: transparent;
    color: var(--fui-color-text);
    cursor: pointer;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  /* Hovered/picked row sits one elevation level above the panel. */
  .item:hover:not(:disabled),
  .item.highlighted,
  .item.selected {
    background: var(--fui-elev);
  }
  .item.selected {
    font-weight: 500;
  }
  .item:focus-visible {
    outline: none;
    box-shadow: var(--fui-focus-ring);
  }
  .item:disabled {
    opacity: var(--fui-opacity-muted);
    cursor: not-allowed;
  }
</style>
