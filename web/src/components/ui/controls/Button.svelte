<script lang="ts">
  import type { Snippet } from 'svelte'
  import { squircle } from '../../../lib/squircle'
  import type { Size } from '../tokens'
  import { tintFill, tintSoft } from '../../../lib/tint'
  import { theme } from '../../../lib/theme.svelte'

  interface ButtonIcon {
    /** Material Symbols ligature name */
    name?: string
    /** image URL, takes precedence over name */
    src?: string
    placement?: 'left' | 'right'
  }

  // One button widget. style: 'none' is the neutral fill (former secondary),
  // 'prominent' the accent fill (former primary), 'text' the borderless
  // TextButton: transparent idle, page or tint text, accent fill on hover.
  // Icon-only chrome (squircle square) derives itself: icon set with neither
  // text nor children. style 'text' applies on top of the icon geometry.
  // tint recolors the fill (or the text, for style 'text') via inline
  // --tint-* vars; style 'none' ignores tint — unless selected is set:
  // selected=true forces the prominent fill, selected=false paints a soft
  // tint wash (toggle-chip pattern), both driven by the button itself.
  let {
    text = '',
    icon,
    title = '',
    tint,
    style = 'none',
    selected,
    size = 'medium',
    disabled = false,
    ariaLabel,
    onclick,
    onmousedown,
    children,
    class: cls = '',
  } = $props<{
    text?: string
    icon?: ButtonIcon
    title?: string
    tint?: string
    style?: 'prominent' | 'none' | 'text'
    selected?: boolean
    size?: Size
    disabled?: boolean
    ariaLabel?: string
    onclick?: (e: MouseEvent) => void
    onmousedown?: (e: MouseEvent) => void
    children?: Snippet
    class?: string
  }>()

  let iconMode = $derived(
    (icon?.name != null || icon?.src != null) && !text && !children
  )
  // Selected state wins over style: on = prominent fill, off = soft wash.
  let effStyle = $derived(
    selected === true ? 'prominent' : selected === false ? 'soft' : style
  )
  let styleClass = $derived(
    iconMode
      ? effStyle === 'text'
        ? 'btn-icon btn-text'
        : effStyle === 'soft'
          ? 'btn-icon btn-soft'
          : effStyle === 'prominent'
            ? 'btn-icon btn-primary'
            : 'btn-icon'
      : effStyle === 'prominent'
        ? 'btn-primary'
        : effStyle === 'text'
          ? 'btn-text'
          : effStyle === 'soft'
            ? 'btn-soft'
            : 'btn-secondary'
  )
  let sizeClass = $derived(
    size === 'small' ? 'btn-small ctl-small' : size === 'large' ? 'btn-large ctl-large' : 'ctl-medium'
  )

  // Icon+text: the icon glyph carries optical side bearings, so the icon edge
  // reads wider than the text edge — .btn-iconed-* pulls that side back.
  let iconSide = $derived(!iconMode && icon ? (icon.placement ?? 'left') : null)

  let tintVars = $derived.by(() => {
    if (!tint) return null
    if (selected === false) {
      void theme.value
      const dark = typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
      const s = tintSoft(tint, dark)
      return `--tint-bg:${s.bg};--tint-hover:${s.bg};--tint-text:${s.text}`
    }
    if (!iconMode && style === 'none') return null
    if (style === 'text') {
      return `--tint-bg:${tint}`
    }
    const f = tintFill(tint)
    return `--tint-bg:${f.bg};--tint-hover:${f.hover};--tint-text:${f.text}`
  })

  let radius = $derived(size === 'small' ? 8 : 12)

  // Sentence case by contract: first letter uppercase, the rest untouched.
  let label = $derived(text ? text[0].toUpperCase() + text.slice(1) : text)
</script>

  <button
  class="btn {styleClass} {sizeClass} {cls}"
  class:btn-tinted={tintVars !== null}
  class:btn-iconed-left={iconSide === 'left'}
  class:btn-iconed-right={iconSide === 'right'}
  style={tintVars ?? ''}
  type="button"
  {disabled}
  aria-label={ariaLabel}
  {title}
  {onclick}
  {onmousedown}
  use:squircle={radius}
>
  {#if iconMode}
    {#if icon?.src}
      <img class="btn-glyph" src={icon.src} alt="" />
    {:else}
      <span class="icon">{icon?.name ?? 'add'}</span>
    {/if}
  {:else}
    {#if icon && (icon.placement ?? 'left') === 'left'}
      {#if icon.src}
        <img class="btn-glyph" src={icon.src} alt="" />
      {:else}
        <span class="icon">{icon.name}</span>
      {/if}
    {/if}
    {#if children}{@render children()}{:else}{label}{/if}
    {#if icon && icon.placement === 'right'}
      {#if icon.src}
        <img class="btn-glyph" src={icon.src} alt="" />
      {:else}
        <span class="icon">{icon.name}</span>
      {/if}
    {/if}
  {/if}
</button>
