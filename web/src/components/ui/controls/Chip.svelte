<script lang="ts">
  // Static tag. Never interactive: toggling is a small Button's job.
  // color is a free-form chip-* class, size follows the control scale.
  // Corners come from startAutoSquircle (main.ts), no manual action here.
  let {
    icon = '',
    text = '',
    iconSide = 'left',
    color = 'chip-neutral',
    size = 'medium',
    title = '',
  } = $props<{
    icon?: string
    text?: string
    iconSide?: 'left' | 'right'
    color?: string
    size?: 'small' | 'medium' | 'large'
    title?: string
  }>()

  const sizeClass = $derived(size === 'small' ? 'chip-sm' : size === 'large' ? 'chip-lg' : '')
</script>

<span class="chip {sizeClass} {color}" class:chip-icon-only={!text} {title}>
  {#if icon && iconSide === 'left'}<span class="icon chip-glyph">{icon}</span>{/if}
  {#if text}<span class="chip-label">{text}</span>{/if}
  {#if icon && iconSide === 'right'}<span class="icon chip-glyph">{icon}</span>{/if}
</span>

<style>
  .chip-label {
    line-height: 1;
  }
</style>
