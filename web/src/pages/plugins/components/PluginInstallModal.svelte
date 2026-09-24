<script lang="ts">
  import { VStack, Text, Chip } from '$ui'
  import type { PluginFacts } from './plugin-facts'
  import { t } from '$lib/i18n.svelte'

  let { facts } = $props<{ facts: PluginFacts }>()
</script>

<VStack gap={4}>
  {#if facts.description}
    <Text size="base">{facts.description}</Text>
  {/if}
  {#if facts.versionLine}
    <Text size="xs" tone="soft">{facts.versionLine}</Text>
  {/if}

  <VStack gap={2}>
    <Text tag="h3" size="sm" weight="bold">{t('Requested permissions')}</Text>
    {#if facts.unsafe}
      <div><Chip text={t('Unrestricted network')} color="chip-red" size="small" /></div>
    {/if}
    {#if facts.allowHosts.length === 0}
      <Text size="sm" tone="soft">{t('No network hosts.')}</Text>
    {:else}
      <VStack gap={1}>
        {#each facts.allowHosts as host}
          <Text size="xs" mono tone="soft">{host}</Text>
        {/each}
      </VStack>
    {/if}
  </VStack>
</VStack>
