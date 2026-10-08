<script lang="ts">
  import type { Snippet } from 'svelte'
  import VStack from '../FUI/layout/VStack.svelte'
  import Text from '../FUI/controls/Text.svelte'

  // Shared auth shell for Bootstrap + Login. Owns the page wrap, card box,
  // brand mark, title and subtitle so the two pages share one stylesheet.
  let {
    title,
    subtitle,
    children,
  } = $props<{
    title: string
    subtitle?: string
    children?: Snippet
  }>()
</script>

<div class="auth-page">
  <div class="auth-card">
    <VStack gap={4}>
      <VStack gap={1}>
        <Text size="sm" weight="bold" class="brand">llm-router</Text>
        <Text tag="h1" size="xl" weight="medium">{title}</Text>
        {#if subtitle}<Text size="base" tone="soft">{subtitle}</Text>{/if}
      </VStack>
      {@render children?.()}
    </VStack>
  </div>
</div>

<style>
  .auth-page {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--fui-space-6);
    background: var(--fui-color-background);
  }
  .auth-card {
    background: var(--fui-color-surface-container-high);
    border-radius: var(--fui-radius-lg);
    padding: var(--fui-space-6);
    width: 100%;
    max-width: 400px;
  }
  :global(.brand) {
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
</style>
