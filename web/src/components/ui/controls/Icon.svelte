<script lang="ts">
  // Font-glyph icon (Material Symbols ligature). The `.icon` base class
  // (level 0) owns the font; size/tone are optional token overrides —
  // unset inherits from context, like a raw ligature span.
  let {
    name,
    size,
    tone,
    class: cls = '',
  } = $props<{
    /** Material Symbols ligature name */
    name: string
    size?: 'xs' | 'sm' | 'base' | 'md' | 'lg' | 'xl' | '2xl'
    tone?: 'default' | 'soft' | 'disabled' | 'accent' | 'danger' | 'success' | 'warning'
    class?: string
  }>()

  const SIZE: Record<string, string> = {
    xs: 'var(--text-xs)',
    sm: 'var(--text-sm)',
    base: 'var(--text-base)',
    md: 'var(--text-md)',
    lg: 'var(--text-lg)',
    xl: 'var(--text-xl)',
    '2xl': 'var(--text-2xl)',
  }
  const TONE: Record<string, string> = {
    default: 'var(--color-text)',
    soft: 'var(--color-text-soft)',
    disabled: 'var(--color-text-disabled)',
    accent: 'var(--color-accent)',
    danger: 'var(--color-error-text)',
    success: 'var(--color-success-text)',
    warning: 'var(--color-warning-text)',
  }
</script>

<span
  class="icon {cls}"
  aria-hidden="true"
  style:font-size={size ? SIZE[size] : undefined}
  style:color={tone ? TONE[tone] : undefined}
>{name}</span>
