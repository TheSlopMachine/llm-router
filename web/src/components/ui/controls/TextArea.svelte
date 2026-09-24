<script lang="ts">
  // Auto-growing multi-line field. Grows with content up to maxRows, then
  // scrolls -- no leading icon, no trailing buttons, no animation: just a
  // plain expanding text box.
  import { squircle } from '../../../lib/squircle'

  let {
    value = $bindable(''),
    hint = '',
    disabled = false,
    required = false,
    minRows = 2,
    maxRows = 10,
    id,
    ariaLabel,
    onchange,
    class: cls = '',
    ...rest
  } = $props<{
    value?: string
    hint?: string
    disabled?: boolean
    required?: boolean
    minRows?: number
    maxRows?: number
    id?: string
    ariaLabel?: string
    onchange?: (value: string) => void
    class?: string
    [key: string]: unknown
  }>()

  let el = $state<HTMLTextAreaElement>()

  function resize(): void {
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }

  // Re-measure whenever the bound value changes -- covers programmatic
  // updates (e.g. a reset button) as well as typing.
  $effect(() => {
    void value
    resize()
  })
</script>

<div class="text-area {cls}" class:disabled>
  <div class="text-area-bg" use:squircle={12} aria-hidden="true"></div>
  <textarea
    bind:this={el}
    {id}
    {disabled}
    {required}
    placeholder={hint}
    rows={minRows}
    style:max-height="{maxRows * 1.5}em"
    bind:value
    oninput={resize}
    onchange={(e) => onchange?.((e.target as HTMLTextAreaElement).value)}
    aria-label={ariaLabel}
    {...rest}
  ></textarea>
</div>

<style>
  .text-area {
    position: relative;
    display: flex;
    padding: var(--field-pad-v) var(--field-pad-h);
    border: 1px solid transparent;
    border-radius: var(--ctl-radius);
  }
  .text-area-bg {
    position: absolute;
    inset: 0;
    border-radius: var(--ctl-radius);
    background: var(--elev);
    transition: background 0.2s ease;
  }
  .text-area > :not(.text-area-bg) {
    position: relative;
  }
  .text-area > :not(.text-area-bg) {
    position: relative;
  }
  .text-area.disabled {
    opacity: 0.6;
  }
  .text-area:focus-within .text-area-bg {
    background:
      linear-gradient(var(--color-accent-soft), var(--color-accent-soft)),
      var(--elev);
  }
  .text-area textarea {
    flex: 1;
    min-width: 0;
    resize: none;
    padding: 0;
    border: none;
    /* No radius here: this element is a scroll container (overflow-y),
       so any border-radius would clip its own text. Corners belong to
       the background layer below. */
    border-radius: 0;
    background: transparent;
    box-shadow: none;
    font: inherit;
    font-size: var(--text-base);
    line-height: 1.5;
    color: var(--color-text);
    overflow-y: auto;
  }
  .text-area textarea:focus {
    background: transparent;
    box-shadow: none;
    outline: none;
  }
</style>
