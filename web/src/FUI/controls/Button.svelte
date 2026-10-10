<script lang="ts">
  import Icon from './Icon.svelte'
  import type { Snippet } from 'svelte'
  import { squircleAuto } from '../core/squircle-baked'
  import type { Size } from '../tokens'
  import { tintFill, tintSoft } from '../core/tint'
  import { theme } from '../core/theme.svelte'
  import { resolveSelectedStyle, resolveButtonVariant, isIconOnly, hasLeftIcon, hasRightIcon } from '../core/button-state'
  import { fillStyle, type FillSize } from '../tokens'
  import { getToolbarCtx, getToolbarPlacement } from '../composite/toolbar.svelte'

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
    active = false,
    ariaExpanded,
    size = 'medium',
    block = false,
    fill,
    collapse = true,
    embedded = false,
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
    active?: boolean
    ariaExpanded?: boolean
    size?: Size
    /** stretch to the full width of the parent */
    block?: boolean
    fill?: FillSize
    /** allow collapsing to icon-only inside Toolbar */
    collapse?: boolean
    /** compact 22px tile for use inside another control (TextEdit trailing actions) */
    embedded?: boolean
    disabled?: boolean
    ariaLabel?: string
    onclick?: (e: MouseEvent) => void
    onmousedown?: (e: MouseEvent) => void
    children?: Snippet
    class?: string
  }>()

  // Inside a Toolbar: icon+text buttons drop the text from level 1 up (text moves to title/aria-label);
  // inside the Toolbar menu a button is a full-width row.
  const tb = getToolbarCtx()
  const placement = getToolbarPlacement()
  const textHidden = $derived(!!tb && placement === 'panel' && collapse && tb.collapse && !!icon && !!text && tb.level >= 1)
  const effText = $derived(textHidden ? '' : text)
  const isBlock = $derived(block || placement === 'menu')
  const inMenu = placement === 'menu'

  let iconMode = $derived(isIconOnly(icon, effText, children !== undefined))
  // Selected state wins over style: on = prominent fill, off = soft wash.
  let effStyle = $derived(resolveSelectedStyle(selected, style))
  // Variant drives colour only; geometry comes from the iconMode / size flags.
  let variant = $derived(resolveButtonVariant(placement === 'menu' ? 'text' : effStyle, iconMode))

  // Icon+text: the icon glyph carries optical side bearings, so the icon edge
  // reads wider than the text edge — the iconed-* flags pull that side back.
  let iconSide = $derived(!iconMode && icon ? (icon.placement ?? 'left') : null)

  let tintVars = $derived.by(() => {
    if (!tint) return null
    if (selected === false) {
      void theme.value
      const dark = typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
      const s = tintSoft(tint, dark)
      return `--fui-tint-bg:${s.bg};--fui-tint-hover:${s.bg};--fui-tint-text:${s.text}`
    }
    if (!iconMode && style === 'none') return null
    if (style === 'text') {
      return `--fui-tint-bg:${tint}`
    }
    if (!tint.startsWith('#')) {
      return `--fui-tint-bg:${tint};--fui-tint-hover:${tint};--fui-tint-text:#ffffff`
    }
    const f = tintFill(tint)
    return `--fui-tint-bg:${f.bg};--fui-tint-hover:${f.hover};--fui-tint-text:${f.text}`
  })


  // Sentence case by contract: first letter uppercase, the rest untouched.
  let label = $derived(effText ? effText[0].toUpperCase() + effText.slice(1) : effText)
</script>

<button
  class="btn {cls}"
  data-variant={variant}
  data-size={size}
  data-tinted={tintVars !== null ? '' : undefined}
  class:icon-only={iconMode}
  class:block={isBlock}
  class:in-menu={inMenu}
  class:embedded
  class:iconed-left={iconSide === 'left'}
  class:iconed-right={iconSide === 'right'}
  class:active={active}
  style:flex={isBlock ? undefined : fillStyle(fill)}
  style:min-width={isBlock || fill == null || fill === false ? undefined : '0'}
  style={tintVars ?? ''}
  type="button"
  {disabled}
  aria-label={ariaLabel ?? (textHidden ? text : undefined)}
  aria-expanded={ariaExpanded ?? (active ? true : undefined)}
  title={title || (textHidden ? text : '')}
  {onclick}
  {onmousedown}
  use:squircleAuto={{ bake: iconMode && !isBlock && !fill ? `btn-icon-${size}` : undefined, fonts: true }}
>
  {#if iconMode}
    {#if icon?.src}
      <img class="glyph" src={icon.src} alt="" />
    {:else}
      <Icon name={icon?.name ?? 'add'} />
    {/if}
  {:else}
    {#if icon && hasLeftIcon(icon)}
      {#if icon.src}
        <img class="glyph" src={icon.src} alt="" />
      {:else}
        <Icon name={icon.name} />
      {/if}
    {/if}
    {#if children}{@render children()}{:else}{label}{/if}
    {#if icon && hasRightIcon(icon)}
      {#if icon.src}
        <img class="glyph" src={icon.src} alt="" />
      {:else}
        <Icon name={icon.name} />
      {/if}
    {/if}
  {/if}
</button>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  :where(button:disabled) { cursor: not-allowed; }
  /* Button owns its entire look. Nothing here depends on app.css except
     tokens (--color-*, --text-*, --btn-pad-*, --ctl-*, --focus-ring*).
     Colour is driven by four custom properties set per variant, so every
     state rule is written once instead of once per variant. */
  .btn {
    --_bg: transparent;
    --_bg-hover: var(--fui-color-button-container-high);
    --_bg-focus: var(--_bg-hover);
    --_fg: var(--fui-color-text-on-button);
    --_ring: var(--fui-focus-ring);

    font-family: inherit;
    font-size: var(--fui-text-base);
    font-weight: 500;
    line-height: var(--fui-btn-line-md);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    /* horizontal rhythm: icon-text gap is half the edge padding */
    gap: calc(var(--fui-btn-pad-h) / 2);
    height: var(--fui-ctl-medium);
    padding: var(--fui-btn-pad-v) var(--fui-btn-pad-h);
    border: var(--fui-border-w) solid transparent;
    border-radius: var(--fui-ctl-radius);
    background: var(--_bg);
    color: var(--_fg);
    cursor: pointer;
    white-space: nowrap;
    user-select: none;
    transition:
      transform var(--fui-dur-fast) ease,
      background var(--fui-dur-base) ease;
  }

  /* ── variants ───────────────────────────────────────────────────────── */
  .btn[data-variant='prominent'] {
    --_bg: var(--fui-color-accent);
    --_bg-hover: var(--fui-color-accent-hover);
    --_fg: var(--fui-color-text-on-button-reverse);
    --_ring: var(--fui-focus-ring-contrast);
  }
  .btn[data-variant='neutral'] {
    --_bg: var(--fui-elev);
    --_bg-hover: var(--fui-color-outline-light);
    --_bg-focus: var(--fui-color-button-container-high);
  }
  /* TextButton (SwiftUI borderless): transparent idle, accent wash on hover. */
  .btn[data-variant='text'] {
    --_fg: var(--fui-color-text);
    --_bg-hover: color-mix(in srgb, var(--fui-color-accent) 15%, transparent);
  }
  /* Selected-off toggle: soft tint wash, hover deepens toward the tint text. */
  .btn[data-variant='soft'] {
    --_bg: var(--fui-tint-bg, var(--fui-elev));
    --_bg-hover: color-mix(in srgb, var(--fui-tint-bg, var(--fui-elev)) 70%, var(--fui-tint-text, currentColor));
    --_fg: var(--fui-tint-text, var(--fui-color-text-on-button));
  }
  /* Tint: colours arrive as inline --tint-* vars computed by lib/tint.ts.
     :where() keeps specificity equal to the variant rules above, so the
     disabled rule below still wins. */
  .btn[data-tinted]:where(:not([data-variant='text']):not([data-variant='soft'])) {
    --_bg: var(--fui-tint-bg);
    --_bg-hover: var(--fui-tint-hover);
    --_fg: var(--fui-tint-text);
  }
  .btn[data-variant='text']:where([data-tinted]) {
    --_fg: var(--fui-tint-bg);
    --_bg-hover: color-mix(in srgb, var(--fui-tint-bg) 15%, transparent);
  }

  /* ── states ─────────────────────────────────────────────────────────── */
  .btn:is(:hover, .active, [aria-expanded='true']):not(:disabled):not([data-variant='prominent']) {
    --_bg: var(--_bg-hover);
  }
  .btn:focus-visible {
    outline: none;
    --_bg: var(--_bg-focus);
    box-shadow: var(--_ring);
  }
  /* press feedback: a small inward bounce */
  .btn:active:not(:disabled) {
    transform: scale(0.96);
  }
  .btn:disabled {
    --_bg: var(--fui-color-disabled-bg);
    --_fg: var(--fui-color-disabled-text);
    cursor: not-allowed;
    opacity: var(--fui-opacity-dim);
  }
  :global(.dark) .btn:disabled {
    opacity: var(--fui-opacity-hover);
  }

  /* ── sizes: same shape, one scale factor for font, padding and radius ── */
  .btn[data-size='small'] {
    height: var(--fui-ctl-small);
    font-size: var(--fui-text-sm);
    line-height: var(--fui-btn-line-sm);
    padding: calc(var(--fui-btn-pad-v) * 0.9) calc(var(--fui-btn-pad-h) * 0.85);
    border-radius: calc(var(--fui-ctl-radius) * 0.85);
  }
  .btn[data-size='large'] {
    height: var(--fui-ctl-large);
    font-size: var(--fui-text-md);
    line-height: var(--fui-btn-line-lg);
    padding: calc(var(--fui-btn-pad-v) * 1.33) calc(var(--fui-btn-pad-h) * 1.15);
    border-radius: calc(var(--fui-ctl-radius) * 1.15);
  }

  /* ── geometry flags ─────────────────────────────────────────────────── */
  /* Icon-only: squircle tile, width pinned to line box + padding. */
  .btn.icon-only {
    padding: var(--fui-btn-pad-v);
    width: calc(var(--fui-btn-pad-v) * 2 + var(--fui-btn-icon-md));
    border-radius: var(--fui-ctl-radius);
  }
  .btn.icon-only :global(.icon) {
    line-height: inherit;
  }
  .btn.icon-only[data-size='small'] {
    /* 18px line box + 2*0.9*pad-v + 2px border */
    width: calc(var(--fui-btn-pad-v) * 1.8 + var(--fui-btn-icon-sm));
  }
  .btn.icon-only[data-size='small'] :global(.icon) {
    font-size: var(--fui-text-md);
  }
  /* Font icons carry ~4px of optical side bearing: pull that side back. */
  .btn.iconed-left {
    padding-left: calc(var(--fui-btn-pad-h) - var(--fui-btn-icon-inset));
  }
  .btn.iconed-right {
    padding-right: calc(var(--fui-btn-pad-h) - var(--fui-btn-icon-inset));
  }
  /* Toolbar menu: a plain list row — text look, own height, full width, left-aligned */
  .btn.in-menu { justify-content: flex-start; }
  .btn.block {
    display: flex;
    width: 100%;
  }
  /* Compact tile for use inside another control (TextEdit actions). */
  .btn.embedded {
    --_fg: var(--fui-color-text-soft);
    --_bg-hover: color-mix(in srgb, var(--fui-color-accent) 12%, var(--fui-elev));
    width: var(--fui-btn-icon-md);
    min-width: var(--fui-btn-icon-md);
    height: var(--fui-btn-icon-md);
    min-height: var(--fui-btn-icon-md);
    padding: 0;
  }
  .btn.embedded:is(:hover, :focus-visible):not(:disabled) {
    --_fg: var(--fui-color-accent);
  }

  /* Image glyph inside a Button (icon={{ src }}); font icons size themselves. */
  .glyph {
    width: var(--fui-btn-icon-sm);
    height: var(--fui-btn-icon-sm);
    object-fit: contain;
  }
</style>
