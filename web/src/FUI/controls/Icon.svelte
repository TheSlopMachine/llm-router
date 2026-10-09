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
    xs: 'var(--fui-text-xs)',
    sm: 'var(--fui-text-sm)',
    base: 'var(--fui-text-base)',
    md: 'var(--fui-text-md)',
    lg: 'var(--fui-text-lg)',
    xl: 'var(--fui-text-xl)',
    '2xl': 'var(--fui-text-2xl)',
  }
  const TONE: Record<string, string> = {
    default: 'var(--fui-color-text)',
    soft: 'var(--fui-color-text-soft)',
    disabled: 'var(--fui-color-text-disabled)',
    accent: 'var(--fui-color-accent)',
    danger: 'var(--fui-color-error-text)',
    success: 'var(--fui-color-success-text)',
    warning: 'var(--fui-color-warning-text)',
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
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  @font-face {
    font-family: "Material Symbols Outlined";
    font-style: normal;
    font-weight: 300;
    font-display: block;
    src: url('../assets/material-symbols-outlined.woff2') format('woff2');
  }
  /* The only place the icon font is declared (@font-face below; the font file lives in ../assets). Every glyph in the app goes through this component. */
  .icon {
    font-family: "Material Symbols Outlined";
    font-size: var(--fui-text-lg);
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
    animation: icon-spin var(--fui-dur-spin) linear infinite;
  }
  @keyframes icon-spin {
    to { transform: rotate(360deg); }
  }
</style>
