<script lang="ts">
  // Auto-growing multi-line field. Grows with content up to maxRows, then
  // scrolls -- no leading icon, no trailing buttons, no animation: just a
  // plain expanding text box.
  import { squircle } from '../core/squircle'
  import { fillStyle, type FillSize } from '../tokens'

  let {
    value = $bindable(''),
    hint = '',
    disabled = false,
    required = false,
    minRows = 4,
    maxRows = 10,
    id,
    ariaLabel,
    onchange,
    fill,
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
    fill?: FillSize
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

<div class="text-area {cls}" class:disabled style:flex={fillStyle(fill)} style:min-width={fill == null || fill === false ? undefined : '0'}>
  <div class="text-area-bg" use:squircle aria-hidden="true"></div>
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
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(textarea) { font: inherit; color: inherit; }
  .text-area {
    position: relative;
    display: flex;
    padding: var(--fui-field-pad-v) var(--fui-field-pad-h);
    border: var(--fui-border-w) solid transparent;
    border-radius: var(--fui-ctl-radius);
  }
  .text-area-bg {
    position: absolute;
    inset: 0;
    border-radius: var(--fui-ctl-radius);
    background: var(--fui-elev);
    transition: background var(--fui-dur-base) ease;
  }
  .text-area > :not(.text-area-bg) {
    position: relative;
  }
  .text-area.disabled {
    opacity: var(--fui-opacity-dim);
  }
  .text-area:focus-within .text-area-bg {
    background:
      linear-gradient(var(--fui-color-accent-soft), var(--fui-color-accent-soft)),
      var(--fui-elev);
  }
  .text-area textarea {
    flex: 1;
    min-width: 0;
    resize: none;
    min-height: 0;
    padding: 0;
    border: none;
    /* No radius here: this element is a scroll container (overflow-y),
       so any border-radius would clip its own text. Corners belong to
       the background layer below. */
    border-radius: 0;
    background: transparent;
    box-shadow: none;
    font: inherit;
    font-size: var(--fui-text-base);
    line-height: 1.5;
    color: var(--fui-color-text);
    overflow-y: auto;
  }
  .text-area textarea:focus {
    background: transparent;
    box-shadow: none;
    outline: none;
  }
</style>
