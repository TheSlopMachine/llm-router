<script lang="ts">
  // Typography addressed by token. Every size/weight/colour the system allows
  // is a named value here, so `font-size: var(--fui-text-sm)` has nowhere to enter from.
  import type { Snippet } from 'svelte'

  let {
    size = 'base',
    weight = 'normal',
    tone = 'default',
    align,
    truncate = false,
    lines,
    break: allowBreak = false,
    mono = false,
    tag = 'span',
    text = '',
    title,
    class: cls = '',
    children,
    ...rest
  } = $props<{
    size?: 'xs' | 'sm' | 'base' | 'md' | 'lg' | 'xl' | '2xl'
    weight?: 'normal' | 'medium' | 'bold'
    tone?: 'default' | 'soft' | 'disabled' | 'accent' | 'danger' | 'success' | 'warning'
    align?: 'left' | 'center' | 'right'
    truncate?: boolean
    /** clamp to N lines; overrides truncate */
    lines?: number
    break?: boolean
    mono?: boolean
    tag?: 'span' | 'p' | 'h1' | 'h2' | 'h3' | 'h4' | 'div' | 'label' | 'code'
    /** plain-string content; use children for markup */
    text?: string
    title?: string
    class?: string
    children?: Snippet
    [key: string]: unknown
  }>()
</script>

<svelte:element
  this={tag}
  class="txt txt-{size} txt-{weight} txt-{tone} {cls}"
  class:txt-mono={mono}
  class:txt-left={align === 'left'}
  class:txt-center={align === 'center'}
  class:txt-right={align === 'right'}
  class:txt-truncate={truncate && lines === undefined}
  class:txt-clamp={lines !== undefined}
  class:txt-break={allowBreak}
  style:--fui-txt-lines={lines}
  {title}
  {...rest}
>
  {#if children}{@render children()}{:else}{text}{/if}
</svelte:element>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  /* Text: every size/weight/colour combination the system allows, addressed
     by token rather than by literal. Scoped here: Text is the sole renderer
     of txt-* classes. `mono` used to emit the global .mono class (which also
     forced font-size); it now uses the scoped .txt-mono below. */
  .txt { margin: 0; min-width: 0; }
  .txt-xs   { font-size: var(--fui-text-xs);   line-height: var(--fui-leading-tight); }
  .txt-sm   { font-size: var(--fui-text-sm);   line-height: var(--fui-leading-base); }
  .txt-base { font-size: var(--fui-text-base); line-height: var(--fui-leading-base); }
  .txt-md   { font-size: var(--fui-text-md);   line-height: var(--fui-leading-tight); }
  .txt-lg   { font-size: var(--fui-text-lg);   line-height: var(--fui-leading-tight); }
  .txt-xl   { font-size: var(--fui-text-xl);   line-height: var(--fui-leading-tight); }
  .txt-2xl  { font-size: var(--fui-text-2xl);  line-height: var(--fui-leading-none); }

  .txt-normal { font-weight: var(--fui-weight-normal); }
  .txt-medium { font-weight: var(--fui-weight-medium); }
  .txt-bold   { font-weight: var(--fui-weight-bold); }

  .txt-mono   { font-family: var(--fui-font-mono); }

  .txt-default  { color: var(--fui-color-text); }
  .txt-soft     { color: var(--fui-color-text-soft); }
  .txt-disabled { color: var(--fui-color-text-disabled); }
  .txt-accent   { color: var(--fui-color-accent); }
  .txt-danger   { color: var(--fui-color-error-text); }
  .txt-success  { color: var(--fui-color-success-text); }
  .txt-warning  { color: var(--fui-color-warning-text); }

  .txt-left   { text-align: left; }
  .txt-center { text-align: center; }
  .txt-right  { text-align: right; }

  .txt-truncate {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .txt-clamp {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: var(--fui-txt-lines, 2);
    line-clamp: var(--fui-txt-lines, 2);
    overflow: hidden;
  }
  .txt-break { overflow-wrap: anywhere; }
</style>
