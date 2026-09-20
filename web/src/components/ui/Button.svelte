<script lang="ts">
  import type { Snippet } from 'svelte'
  import { squircle } from '../../lib/squircle'
  import { tintFill, tintText } from '../../lib/tint'
  import { theme } from '../../lib/theme.svelte'

  interface ButtonIcon {
    /** Material Symbols ligature name */
    name?: string
    /** image URL, takes precedence over name */
    src?: string
    placement?: 'left' | 'right'
  }

  // One button widget. style: 'none' is the neutral fill (former secondary),
  // 'prominent' the accent fill (former primary), 'text' the borderless
  // TextButton, 'icon' the squircle-square icon button. tint recolors the
  // fill (or the text, for style 'text') via inline --tint-* vars.
  let {
    text = '',
    icon,
    glyphClass = '',
    title = '',
    tint,
    style = 'none',
    size = 'base',
    disabled = false,
    danger = false,
    type = 'button',
    ariaLabel,
    onclick,
    children,
  } = $props<{
    text?: string
    icon?: ButtonIcon
    /** extra class on the glyph span (spin, status colors) */
    glyphClass?: string
    title?: string
    tint?: string
    style?: 'prominent' | 'none' | 'text' | 'icon'
    size?: 'sm' | 'base' | 'lg'
    disabled?: boolean
    /** red glyph + reddish hover fill, icon style only */
    danger?: boolean
    type?: 'button' | 'submit'
    ariaLabel?: string
    onclick?: (e: MouseEvent) => void
    children?: Snippet
  }>()

  let styleClass = $derived(
    style === 'prominent'
      ? 'btn-primary'
      : style === 'text'
        ? 'btn-text'
        : style === 'icon'
          ? 'btn-icon'
          : 'btn-secondary'
  )
  let sizeClass = $derived(size === 'sm' ? 'btn-sm' : size === 'lg' ? 'btn-large' : '')

  // Icon+text: the icon glyph carries optical side bearings, so the icon edge
  // reads wider than the text edge — .btn-iconed-* pulls that side back.
  let iconSide = $derived(style !== 'icon' && icon ? (icon.placement ?? 'left') : null)

  let tintVars = $derived.by(() => {
    if (!tint) return null
    void theme.value // re-derive on theme change
    const dark = document.documentElement.classList.contains('dark')
    if (style === 'text') {
      const c = tintText(tint, dark)
      return `--tint-bg:${c};--tint-soft:color-mix(in srgb, ${c} 12%, transparent)`
    }
    const f = tintFill(tint)
    return `--tint-bg:${f.bg};--tint-hover:${f.hover};--tint-text:${f.text}`
  })

  let radius = $derived(size === 'sm' ? 8 : 12)

  // Sentence case by contract: first letter uppercase, the rest untouched.
  let label = $derived(text ? text[0].toUpperCase() + text.slice(1) : text)
</script>

  <button
  class="btn {styleClass} {sizeClass}"
  class:btn-tinted={tintVars !== null}
  class:icon-danger={danger && style === 'icon'}
  class:btn-iconed-left={iconSide === 'left'}
  class:btn-iconed-right={iconSide === 'right'}
  style={tintVars ?? ''}
  {type}
  {disabled}
  aria-label={ariaLabel}
  {title}
  {onclick}
  use:squircle={radius}
>
  {#if style === 'icon'}
    {#if icon?.src}
      <img class="btn-glyph" src={icon.src} alt="" />
    {:else}
      <span class="icon {glyphClass}">{icon?.name ?? 'add'}</span>
    {/if}
  {:else}
    {#if icon && (icon.placement ?? 'left') === 'left'}
      {#if icon.src}
        <img class="btn-glyph" src={icon.src} alt="" />
      {:else}
        <span class="icon {glyphClass}">{icon.name}</span>
      {/if}
    {/if}
    {#if children}{@render children()}{:else}{label}{/if}
    {#if icon && icon.placement === 'right'}
      {#if icon.src}
        <img class="btn-glyph" src={icon.src} alt="" />
      {:else}
        <span class="icon {glyphClass}">{icon.name}</span>
      {/if}
    {/if}
  {/if}
</button>
