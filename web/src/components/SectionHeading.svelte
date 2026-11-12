<script lang="ts">
  // Section title inside a page: title, optional count / description, actions on the right.
  // The page title itself is a FUI <Header> (drawn by the Scaffold).
  import type { Snippet } from 'svelte'
  import { HStack, VStack, Spacer, Text } from '@fuckyouryan/fui'

  let { title, subtitle, count, actions }: {
    title: string
    subtitle?: string
    /** a number shown right after the title, on its line */
    count?: number
    actions?: Snippet
  } = $props()
</script>

<HStack valign="center" gap={3} wrap>
  <VStack gap={1}>
    <HStack gap={2} valign="center">
      <Text variant="section-title" text={title} />
      {#if count !== undefined}<Text variant="section-title" foregroundStyle="tertiary" text={String(count)} />{/if}
    </HStack>
    {#if subtitle}<Text variant="caption" text={subtitle} />{/if}
  </VStack>
  {#if actions}
    <Spacer />
    {@render actions()}
  {/if}
</HStack>
