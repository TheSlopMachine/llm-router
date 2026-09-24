<script lang="ts">
  // Typography addressed by token. Every size/weight/colour the system allows
  // is a named value here, so `font-size: var(--text-sm)` has nowhere to enter from.
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
  class="txt txt-{size} txt-{weight} txt-{tone} {mono ? 'mono' : ''} {cls}"
  class:txt-left={align === 'left'}
  class:txt-center={align === 'center'}
  class:txt-right={align === 'right'}
  class:txt-truncate={truncate && lines === undefined}
  class:txt-clamp={lines !== undefined}
  class:txt-break={allowBreak}
  style:--txt-lines={lines}
  {title}
  {...rest}
>
  {#if children}{@render children()}{:else}{text}{/if}
</svelte:element>
