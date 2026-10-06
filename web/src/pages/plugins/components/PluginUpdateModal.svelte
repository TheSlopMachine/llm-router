<script lang="ts">
  import { VStack, Text, Chip } from '$ui'
  import { t } from '$lib/i18n.svelte'

  let {
    displayName,
    current,
    latest,
    newHosts,
    newUnsafe,
    added,
    removed,
    escalatesToUnsafe
  } = $props<{
    displayName: string
    current: string
    latest: string
    newHosts: string[]
    newUnsafe: boolean
    added: string[]
    removed: string[]
    escalatesToUnsafe: boolean
  }>()

  let hasChanges = $derived(added.length > 0 || removed.length > 0 || escalatesToUnsafe)
</script>

<VStack gap={4}>
  <Text size="sm" tone="soft">{displayName} · v{current} → v{latest}</Text>

  {#if escalatesToUnsafe}
    <VStack gap={1}>
      <div><Chip text={t('plugins.network.unrestricted')} color="chip-red" size="small" /></div>
      <Text size="sm" tone="danger">{t('plugins.update.this_requests_unrestricted')}</Text>
    </VStack>
  {/if}

  {#if added.length > 0}
    <VStack gap={1}>
      <Text tag="h3" size="sm" weight="bold">{t('plugins.update.new_hosts')}</Text>
      {#each added as host}
        <Text size="xs" mono>+ {host}</Text>
      {/each}
    </VStack>
  {/if}

  {#if removed.length > 0}
    <VStack gap={1}>
      <Text tag="h3" size="sm" weight="bold">{t('plugins.update.removed_hosts')}</Text>
      {#each removed as host}
        <Text size="xs" mono tone="soft">− {host}</Text>
      {/each}
    </VStack>
  {/if}

  {#if !hasChanges}
    <VStack gap={1}>
      <Text tag="h3" size="sm" weight="bold">{t('plugins.update.network_permissions')}</Text>
      {#if newUnsafe}
        <div><Chip text={t('plugins.network.unrestricted')} color="chip-red" size="small" /></div>
      {:else if newHosts.length === 0}
        <Text size="sm" tone="soft">{t('plugins.network.none')}</Text>
      {:else}
        {#each newHosts as host}
          <Text size="xs" mono tone="soft">{host}</Text>
        {/each}
      {/if}
    </VStack>
  {:else if !escalatesToUnsafe}
    <VStack gap={1}>
      <Text tag="h3" size="sm" weight="bold">{t('plugins.update.all_hosts')}</Text>
      {#if newUnsafe}
        <div><Chip text={t('plugins.network.unrestricted')} color="chip-red" size="small" /></div>
      {:else}
        {#each newHosts as host}
          <Text size="xs" mono tone="soft">{host}</Text>
        {/each}
      {/if}
    </VStack>
  {/if}
</VStack>
