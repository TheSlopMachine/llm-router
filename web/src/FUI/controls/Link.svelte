<script lang="ts">
  import type { Snippet } from 'svelte'

  // Anchor with library styling. `external` opens in a new tab safely.
  let {
    href,
    text = '',
    external = false,
    class: cls = '',
    children,
    ...rest
  } = $props<{
    href: string
    text?: string
    external?: boolean
    class?: string
    children?: Snippet
    [key: string]: unknown
  }>()
</script>

<a
  class="fui-link {cls}"
  {href}
  target={external ? '_blank' : undefined}
  rel={external ? 'noopener noreferrer' : undefined}
  {...rest}
>{#if children}{@render children()}{:else}{text}{/if}</a>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .fui-link {
    color: var(--fui-color-text-link);
    font: inherit;
    font-weight: var(--fui-weight-medium);
    text-decoration: none;
    cursor: pointer;
  }
  .fui-link:hover {
    text-decoration: underline;
  }
</style>
