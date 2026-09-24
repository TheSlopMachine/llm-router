<script lang="ts">
  // Single-line text field composed from library elements: the background is
  // a squircled layer (clip never touches content), the leading glyph is a
  // Text, every trailing button is a Button. Only the native input stays raw.
  import { untrack } from 'svelte'
  import { squircle } from '../../../lib/squircle'
  import Button from './Button.svelte'
  import Text from './Text.svelte'
  import HStack from '../layout/HStack.svelte'
    import { accent } from '$lib/accent.svelte';

  export interface TrailingAction {
    icon: string
    label: string
    onclick: () => void
    disabled?: boolean
  }

  let {
    value = $bindable(''),
    hint = '',
    leadingIcon,
    trailing = [],
    type = 'text',
    regex,
    disabled = false,
    required = false,
    id,
    ariaLabel,
    onchange,
    onvalid,
    class: cls = '',
    ...rest
  } = $props<{
    value?: string
    /** placeholder text */
    hint?: string
    /** icon name; caller icons collapse on focus, the forced secret lock does not */
    leadingIcon?: string
    /** caller icon-only buttons; slide in on focus, sit left of system buttons */
    trailing?: TrailingAction[]
    type?: 'text' | 'secret'
    /** RegExp source for marking (never blocking); empty is always valid */
    regex?: string
    disabled?: boolean
    required?: boolean
    id?: string
    ariaLabel?: string
    onchange?: (value: string) => void
    onvalid?: (valid: boolean) => void
    class?: string
    [key: string]: unknown
  }>()

  let focused = $state(false)
  let revealed = $state(false)
  const isSecret = $derived(type === 'secret')
  const effectiveType = $derived(isSecret ? (revealed ? 'text' : 'password') : type)

  // Marking only: invalid shows once the field loses focus, never while typing.
  const valid = $derived.by(() => {
    if (!regex || value === '') return true
    try {
      return new RegExp(regex).test(value)
    } catch {
      return true
    }
  })
  const showInvalid = $derived(!focused && !valid)

  $effect(() => {
    void valid
    untrack(() => onvalid?.(valid))
  })

  // Forced chrome: secret lock leads unless the caller passes its own icon
  // (caller icons keep the focus-collapse animation, the lock does not).
  const resolvedLeading = $derived(leadingIcon ?? (isSecret ? 'lock' : undefined))
  const leadingStatic = $derived(isSecret && leadingIcon == null)

  // System trailing pins the right edge, ignores focus. Order: eye, then
  // clear last. Clear shows for non-empty enabled fields only.
  const showClear = $derived(value !== '' && !disabled)

  function clearValue(): void {
    value = ''
    onchange?.('')
  }

  function keepFocus(e: MouseEvent): void {
    e.preventDefault()
  }
</script>

<!-- TextEdit is always large: single height by design. -->
<div class="text-edit ctl-medium {cls}" class:disabled class:invalid={showInvalid}>
  <div class="text-edit-bg" use:squircle={12} aria-hidden="true"></div>
  <div class="text-edit-tint" use:squircle={12} aria-hidden="true"></div>
  {#if resolvedLeading}
    <Text
      class="icon edit-leading{leadingStatic ? ' edit-leading-static' : ''}"
      text={resolvedLeading}
      aria-hidden="true"
      style="margin-right: 6px;"
    />
  {/if}
  <input
    {id}
    type={effectiveType}
    {disabled}
    {required}
    placeholder={hint}
    bind:value
    onfocus={() => { focused = true }}
    onblur={() => { focused = false }}
    onchange={(e) => onchange?.((e.target as HTMLInputElement).value)}
    aria-label={ariaLabel}
    aria-invalid={showInvalid || undefined}
    {...rest}
  />
  {#if trailing.length > 0}
    <HStack gap={0} class="edit-trailing">
      {#each trailing as action (action.icon + action.label)}
        <Button
          size="small"
          icon={{ name: action.icon }}
          title={action.label}
          ariaLabel={action.label}
          disabled={action.disabled}
          onclick={() => action.onclick()}
          onmousedown={keepFocus}
        />
      {/each}
    </HStack>
  {/if}
  {#if isSecret || showClear}
    <HStack gap={0} class="edit-system">
      {#if isSecret}
        <Button
          size="small"
          icon={{ name: revealed ? 'visibility_off' : 'visibility' }}
          title={revealed ? 'Hide secret' : 'Show secret'}
          ariaLabel={revealed ? 'Hide secret' : 'Show secret'}
          disabled={disabled}
          onclick={() => { revealed = !revealed }}
          onmousedown={keepFocus}
        />
      {/if}
      <Button
        size="small"
        icon={{ name: 'close' }}
        title="Clear"
        ariaLabel="Clear"
        onclick={clearValue}
        onmousedown={keepFocus}
        class={showClear ? 'edit-clear' : 'edit-clear edit-clear-hidden'}
      />
    </HStack>
  {/if}
</div>

<style>
  /* Composite field: the background layer wears the fill (squircle-clipped),
     content above it is never clipped. Outer box owns layout only. */
  .text-edit {
    position: relative;
    display: flex;
    align-items: center;
    padding: var(--field-pad-h) var(--field-pad-h);
    border: 1px solid transparent;
    border-radius: var(--ctl-radius);
  }
  .text-edit-bg {
    position: absolute;
    inset: 0;
    border-radius: var(--ctl-radius);
    background: var(--elev);
  }
  /* Tint crossfades fast: background gradients cannot interpolate, so the
     tint lives on its own layer driven by opacity. */
  .text-edit-tint {
    position: absolute;
    inset: 0;
    border-radius: var(--ctl-radius);
    background:
      linear-gradient(var(--color-accent-soft), var(--color-accent-soft)),
      var(--elev);
    opacity: 0;
    transition: opacity 0.15s ease;
  }
  .text-edit > :not(.text-edit-bg):not(.text-edit-tint) {
    position: relative;
  }

  .text-edit.disabled {
    opacity: 0.6;
  }

  /* Focus glow and invalid marking tint the tint layer only. */
  .text-edit:focus-within .text-edit-tint {
    opacity: 1;
  }
  .text-edit.invalid .text-edit-tint {
    opacity: 1;
    background:
      linear-gradient(var(--color-notification-error-bg), var(--color-notification-error-bg)),
      var(--elev);
  }

  .text-edit input {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    /* Square box: a radius here is harmless today (no overflow), but one
       future overflow declaration would turn it into self-clipped text.
       Corners belong to the background layer below. */
    border-radius: 0;
    background: transparent;
    box-shadow: none;
    font-size: var(--text-base);
    line-height: 20px;
    color: var(--color-text);
  }

  .text-edit input:focus {
    background: transparent;
    box-shadow: none;
    outline: none;
  }

  /* Leading glyph: uniform padding on all four sides. Caller icons collapse
     on focus, the forced lock stays. */
  .edit-leading {
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    padding: var(--space-2);
    max-width: 48px;
    overflow: hidden;
    transition:
      max-width 0.2s ease-in,
      padding 0.2s ease-in,
      margin 0.2s ease-in,
      opacity 0.15s ease-in,
      transform 0.15s ease-in;
  }
  .text-edit:focus-within .edit-leading:not(.edit-leading-static) {
    max-width: 0;
    padding: 0;
    opacity: 0;
    transform: scale(0.8);
  }

  /* Trailing rows: the outer box owns the right inset, containers keep
     vertical padding only, every button keeps a left margin, so icon
     spacing is uniform on both axes. */
  .edit-trailing {
    max-width: 0;
    opacity: 0;
    overflow: hidden;
    pointer-events: none;
    transition:
      max-width 0.2s ease-in,
      padding 0.2s ease-in,
      opacity 0.18s ease-in,
      transform 0.18s ease-in;
  }
  .text-edit:focus-within .edit-trailing {
    /* generous ceiling: real width is intrinsic, this only unlocks it */
    max-width: 200px;
    opacity: 1;
    pointer-events: auto;
  }
  .edit-system {
    padding: 0 0;
  }

  /* Embedded icon buttons (iconMode forces btn-icon chrome): fixed 22px
     tile, soft glyph, no double chrome over the field. */
  .edit-trailing :global(.btn),
  .edit-system :global(.btn) {
    height: 22px;
    min-height: 22px;
    width: 22px;
    min-width: 22px;
    padding: 0;
    margin-left: var(--space-2);
  }
  .edit-trailing :global(.btn-icon),
  .edit-system :global(.btn-icon) {
    color: var(--color-text-soft);
  }
  .edit-trailing :global(.btn-icon:hover:not([disabled])),
  .edit-system :global(.btn-icon:hover:not([disabled])) {
    background: color-mix(in srgb, var(--color-accent) 12%, var(--elev));
    color: var(--color-accent);
  }

  /* Clear zoom: ease-in both ways, collapses its slot when hidden. */
  .edit-system :global(.edit-clear) {
    overflow: hidden;
    max-width: 22px;
    transition:
      transform 0.15s ease-in,
      opacity 0.15s ease-in,
      max-width 0.15s ease-in,
      margin 0.15s ease-in;
  }
  .edit-system :global(.edit-clear-hidden) {
    transform: scale(0);
    opacity: 0;
    max-width: 0;
    margin-left: 0;
    pointer-events: none;
  }
</style>
