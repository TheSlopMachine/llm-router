<script lang="ts">
  // Font-glyph icon (Material Symbols ligature). Owns the icon font itself;
  // size/tone are optional token overrides, unset inherits from context.
  let {
    name,
    size,
    tone,
    filled = false,
    spin = false,
    class: cls = '',
  } = $props<{
    /** Material Symbols ligature name */
    name: string
    size?: 'xs' | 'sm' | 'base' | 'md' | 'lg' | 'xl' | '2xl'
    tone?: 'default' | 'soft' | 'disabled' | 'accent' | 'danger' | 'success' | 'warning'
    /** filled glyph variant */
    filled?: boolean
    /** busy indicator: pair with name="progress_activity" */
    spin?: boolean
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
  class:filled
  class:spin
  aria-hidden="true"
  style:font-size={size ? SIZE[size] : undefined}
  style:color={tone ? TONE[tone] : undefined}
>{name}</span>

<style>
  /* The only place the icon font is declared (@font-face lives in
     /fonts/fonts.css). Every glyph in the app goes through this component. */
  .icon {
    font-family: "Material Symbols Outlined";
    font-size: var(--text-lg);
    vertical-align: middle;
    display: inline-block;
    font-weight: normal;
    font-style: normal;
    line-height: 1;
    letter-spacing: normal;
    text-transform: none;
    white-space: nowrap;
    word-wrap: normal;
    direction: ltr;
    -webkit-font-feature-settings: "liga";
    font-feature-settings: "liga";
    font-variation-settings: "FILL" 0, "wght" 300, "GRAD" 0, "opsz" 20;
  }
  .filled {
    font-variation-settings: "FILL" 1, "wght" 300, "GRAD" 0, "opsz" 20;
  }
  .spin {
    animation: icon-spin 1s linear infinite;
  }
  @keyframes icon-spin {
    to { transform: rotate(360deg); }
  }
</style>
