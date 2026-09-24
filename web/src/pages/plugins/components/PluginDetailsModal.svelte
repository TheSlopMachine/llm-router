<script lang="ts">
  import { VStack, HStack, Text, Chip } from '$ui'
  import type { PluginFacts } from './plugin-facts'
  import { t } from '$lib/i18n.svelte'

  let {
    facts,
    logs = [],
    crashes = [],
    loading = false
  } = $props<{
    facts: PluginFacts
    logs?: Array<{ at: string; message: string }>
    crashes?: Array<{ at: string; type_key: string; cause: string }>
    loading?: boolean
  }>()
</script>

<VStack gap={4}>
  {#if facts.description}
    <Text size="base">{facts.description}</Text>
  {/if}

  <HStack align="center" gap={2}>
    <Text size="xs" tone="soft" mono>{facts.idLine}</Text>
    {#if facts.versionLine}
      <Text size="xs" tone="soft">· {facts.versionLine}</Text>
    {/if}
  </HStack>

  {#if facts.typeKeys.length > 0}
    <VStack gap={2}>
      <Text tag="h3" size="sm" weight="bold">{t('Provides')}</Text>
      <HStack gap={2} wrap>
        {#each facts.typeKeys as key}
          <Chip text={key} color="chip-neutral" size="small" />
        {/each}
      </HStack>
    </VStack>
  {/if}

  <VStack gap={2}>
    <Text tag="h3" size="sm" weight="bold">{t('Permissions')}</Text>
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

  {#if logs.length > 0 || crashes.length > 0 || loading}
    <VStack gap={2}>
      <Text tag="h3" size="sm" weight="bold">{t('Recent crashes')}</Text>
      {#if loading}
        <Text size="sm" tone="soft">{t('Loading…')}</Text>
      {:else if crashes.length === 0}
        <Text size="sm" tone="soft">{t('None recorded.')}</Text>
      {:else}
        <VStack gap={1}>
          {#each crashes as crash}
            <Text size="xs"><Text tone="soft">{crash.at} [{crash.type_key}]</Text> {crash.cause}</Text>
          {/each}
        </VStack>
      {/if}
    </VStack>

    <VStack gap={2}>
      <Text tag="h3" size="sm" weight="bold">{t('Recent log output')}</Text>
      {#if loading}
        <Text size="sm" tone="soft">{t('Loading…')}</Text>
      {:else if logs.length === 0}
        <Text size="sm" tone="soft">{t('None recorded.')}</Text>
      {:else}
        <VStack gap={1}>
          {#each logs as log}
            <Text size="xs"><Text tone="soft">{log.at}</Text> {log.message}</Text>
          {/each}
        </VStack>
      {/if}
    </VStack>
  {/if}
</VStack>
