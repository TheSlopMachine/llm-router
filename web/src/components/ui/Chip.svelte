<script lang="ts">
  import { squircle } from '../../lib/squircle'

  // Single tag widget. Static by default; with onclick or pressed it
  // becomes a toggle button with hover/focus/active feedback.
  let {
    icon = '',
    text = '',
    iconSide = 'left',
    cls = 'chip-neutral',
    title = '',
    glyphSize,
    pressed,
    disabled = false,
    onclick
  } = $props<{
    icon?: string
    text?: string
    iconSide?: 'left' | 'right'
    cls?: string
    title?: string
    glyphSize?: number
    pressed?: boolean
    disabled?: boolean
    onclick?: () => void
  }>()

  const interactive = $derived(onclick != null || pressed != null)
</script>

{#if interactive}
  <button
    type="button"
    class="chip chip-sm {cls}"
    class:chip-icon-only={!text}
    class:chip-pressed={pressed === true}
    {title}
    {disabled}
    onclick={() => onclick?.()}
    use:squircle={8}
  >
    {#if icon && iconSide === 'left'}<span class="icon chip-glyph" style:font-size={glyphSize != null ? `${glyphSize}px` : undefined}>{icon}</span>{/if}
    {#if text}<span class="chip-label">{text}</span>{/if}
    {#if icon && iconSide === 'right'}<span class="icon chip-glyph" style:font-size={glyphSize != null ? `${glyphSize}px` : undefined}>{icon}</span>{/if}
  </button>
{:else}
  <span class="chip chip-sm {cls}" class:chip-icon-only={!text} {title} use:squircle={8}>
    {#if icon && iconSide === 'left'}<span class="icon chip-glyph" style:font-size={glyphSize != null ? `${glyphSize}px` : undefined}>{icon}</span>{/if}
    {#if text}<span class="chip-label">{text}</span>{/if}
    {#if icon && iconSide === 'right'}<span class="icon chip-glyph" style:font-size={glyphSize != null ? `${glyphSize}px` : undefined}>{icon}</span>{/if}
  </span>
{/if}

<style>
  .chip-icon-only {
    padding: 3px;
    border-radius: 8px;
  }
  .chip {
    gap: 5px;
  }
  button.chip {
    cursor: pointer;
    border: none;
    font-family: inherit;
    transition:
      transform 0.12s ease,
      background-color 0.15s ease;
  }
  button.chip:hover:not(:disabled) {
    filter: brightness(1.15);
  }
  button.chip:focus-visible {
    outline: none;
    filter: brightness(1.15);
  }
  button.chip:active:not(:disabled) {
    transform: scale(0.95);
  }
  button.chip:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  button.chip-pressed {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  .chip-label {
    line-height: 1;
  }
</style>
