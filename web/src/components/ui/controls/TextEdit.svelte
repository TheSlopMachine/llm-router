<script lang="ts">
  // Single-line text field composed from library elements: the background is
  // a squircled layer (clip never touches content), the leading glyph is a
  // Text, every trailing button is a Button. Only the native input stays raw.
  import { untrack } from 'svelte'
  import { squircle } from '../../../lib/squircle'
  import Button from './Button.svelte'
  import Icon from './Icon.svelte'
  import { resolveInputType } from '../../../lib/button-state'

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
  const effectiveType = $derived(resolveInputType(isSecret, revealed, type))

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

<!-- TextEdit has a single height by design (--ctl-medium). -->
<div class="text-edit {cls}" class:disabled class:invalid={showInvalid}>
  <div class="bg" use:squircle={12} aria-hidden="true"></div>
  <div class="tint" use:squircle={12} aria-hidden="true"></div>
  {#if resolvedLeading}
    <span class="leading" class:leading-static={leadingStatic} aria-hidden="true">
      <Icon name={resolvedLeading} size="base" />
    </span>
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
    <div class="actions trailing">
      {#each trailing as action (action.icon + action.label)}
        <span class="slot">
          <Button
            size="small"
            embedded
            icon={{ name: action.icon }}
            title={action.label}
            ariaLabel={action.label}
            disabled={action.disabled}
            onclick={() => action.onclick()}
            onmousedown={keepFocus}
          />
        </span>
      {/each}
    </div>
  {/if}
  {#if isSecret || showClear}
    <div class="actions">
      {#if isSecret}
        <span class="slot">
          <Button
            size="small"
            embedded
            icon={{ name: revealed ? 'visibility_off' : 'visibility' }}
            title={revealed ? 'Hide secret' : 'Show secret'}
            ariaLabel={revealed ? 'Hide secret' : 'Show secret'}
            disabled={disabled}
            onclick={() => { revealed = !revealed }}
            onmousedown={keepFocus}
          />
        </span>
      {/if}
      <span class="slot clear" class:clear-hidden={!showClear}>
        <Button
          size="small"
          embedded
          icon={{ name: 'close' }}
          title="Clear"
          ariaLabel="Clear"
          onclick={clearValue}
          onmousedown={keepFocus}
        />
      </span>
    </div>
  {/if}
</div>

<style>
  /* Composite field: the background layer wears the fill (squircle-clipped),
     content above it is never clipped. Outer box owns layout only.
     Every element that gets a rule here is a plain element of this
     component: scoped CSS cannot reach a class passed to a child component's
     root, which is how the previous leading/trailing rules went dead. */
  .text-edit {
    position: relative;
    display: flex;
    align-items: center;
    height: var(--ctl-medium);
    padding: var(--field-pad-h) var(--field-pad-h);
    border: 1px solid transparent;
    border-radius: var(--ctl-radius);
  }
  .bg {
    position: absolute;
    inset: 0;
    border-radius: var(--ctl-radius);
    background: var(--elev);
  }
  /* Tint crossfades fast: background gradients cannot interpolate, so the
     tint lives on its own layer driven by opacity. */
  .tint {
    position: absolute;
    inset: 0;
    border-radius: var(--ctl-radius);
    background:
      linear-gradient(var(--color-accent-soft), var(--color-accent-soft)),
      var(--elev);
    opacity: 0;
    transition: opacity 0.15s ease;
  }
  .text-edit > :not(.bg):not(.tint) {
    position: relative;
  }

  .text-edit.disabled {
    opacity: 0.6;
  }

  /* Focus glow and invalid marking tint the tint layer only. */
  .text-edit:focus-within .tint {
    opacity: 1;
  }
  .text-edit.invalid .tint {
    opacity: 1;
    background:
      linear-gradient(var(--color-notification-error-bg), var(--color-notification-error-bg)),
      var(--elev);
  }

  input {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    /* Square box: corners belong to the background layer. A radius here would
       turn into self-clipped text the day someone adds overflow. */
    border-radius: 0;
    background: transparent;
    box-shadow: none;
    outline: none;
    font-family: inherit;
    font-size: var(--text-base);
    line-height: 20px;
    color: var(--color-text);
  }

  /* Leading glyph: uniform padding on all four sides. Caller icons collapse
     on focus, the forced secret lock stays. */
  .leading {
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    padding: var(--space-2);
    margin-right: 6px;
    max-width: 48px;
    overflow: hidden;
    transition:
      max-width 0.2s ease-in,
      padding 0.2s ease-in,
      margin 0.2s ease-in,
      opacity 0.15s ease-in,
      transform 0.15s ease-in;
  }
  .leading :global(.icon) {
    line-height: var(--leading-base);
  }
  .text-edit:focus-within .leading:not(.leading-static) {
    max-width: 0;
    padding: 0;
    margin-right: 0;
    opacity: 0;
    transform: scale(0.8);
  }

  /* Action rows. Each button sits in a .slot that owns its left margin, so
     icon spacing is uniform and the clear button can collapse its own slot. */
  .actions {
    display: flex;
    align-items: center;
  }
  .slot {
    display: inline-flex;
    margin-left: var(--space-2);
  }
  /* Caller trailing actions slide in on focus, left of the system buttons. */
  .trailing {
    max-width: 0;
    opacity: 0;
    overflow: hidden;
    pointer-events: none;
    transition:
      max-width 0.2s ease-in,
      opacity 0.18s ease-in,
      transform 0.18s ease-in;
  }
  .text-edit:focus-within .trailing {
    /* generous ceiling: real width is intrinsic, this only unlocks it */
    max-width: 200px;
    opacity: 1;
    pointer-events: auto;
  }

  /* Clear zoom: ease-in both ways, collapses its slot when hidden. */
  .clear {
    overflow: hidden;
    max-width: 22px;
    transition:
      transform 0.15s ease-in,
      opacity 0.15s ease-in,
      max-width 0.15s ease-in,
      margin 0.15s ease-in;
  }
  .clear-hidden {
    transform: scale(0);
    opacity: 0;
    max-width: 0;
    margin-left: 0;
    pointer-events: none;
  }
</style>
