<script lang="ts">
  import type { Snippet } from 'svelte'
  import Icon from '../controls/Icon.svelte'
  import Text from '../controls/Text.svelte'

  // Shared empty placeholder. Title + optional caption + optional ready-made
  // Button (passed as the `action` snippet — EmptyState never builds buttons
  // itself).
  let {
    icon,
    title,
    caption,
    action,
  } = $props<{
    icon?: string
    title: string
    caption?: string
    action?: Snippet
  }>()
</script>

<div class="fui-empty">
  {#if icon}<Icon name={icon} class="fui-empty-icon" />{/if}
  <Text size="lg" weight="medium" align="center">{title}</Text>
  {#if caption}<Text size="sm" tone="soft" align="center">{caption}</Text>{/if}
  {#if action}{@render action()}{/if}
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .fui-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--fui-space-3);
    padding: var(--fui-space-8);
    text-align: center;
    color: var(--fui-color-text-soft);
    font-size: var(--fui-text-base);
  }
  /* Global: the class rides the Icon root; Svelte drops component-passed
     classes from scoped CSS as unused. */
  :global(.fui-empty-icon) {
    font-size: var(--fui-text-display);
    color: var(--fui-color-text-soft);
  }
</style>
