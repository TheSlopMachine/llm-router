<script lang="ts">
  import type { Snippet } from 'svelte'
  import Icon from './ui/controls/Icon.svelte'
  import Text from './ui/controls/Text.svelte'
  import VStack from './ui/layout/VStack.svelte'

  // Shared empty placeholder. Title + optional caption + optional ready-made
  // Button (passed as the `action` snippet — EmptyState never builds buttons
  // itself). Icon renders through the Icon widget; its display size is the
  // one piece of unique decoration here.
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

<VStack align="center" gap={3} class="empty">
  {#if icon}<Icon name={icon} class="empty-icon" />{/if}
  <Text size="lg" weight="medium" align="center">{title}</Text>
  {#if caption}<Text size="sm" tone="soft" align="center">{caption}</Text>{/if}
  {#if action}{@render action()}{/if}
</VStack>

<style>
  /* Global: the class rides the Icon root, Svelte drops
     component-passed classes from scoped CSS as unused. */
  :global(.empty-icon) {
    font-size: var(--text-display);
    color: var(--color-text-soft);
  }
</style>
