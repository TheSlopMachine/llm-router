<script lang="ts">
  import { VStack, HStack, Text, Chip, Spacer } from '$ui'
  import { t } from '$lib/i18n.svelte'
  import type { UpdateAllRow } from './install-confirm'
  import { hasPermissionChanges } from '$lib/plugin-search'

  let { rows } = $props<{ rows: UpdateAllRow[] }>()
</script>

<VStack gap={4}>
  <Text size="sm" tone="soft">{t('plugins.update.review_hosts')}</Text>
  {#each rows as row (row.pluginId)}
    <VStack gap={1}>
      <HStack align="center" gap={2}>
        <Text size="sm" weight="medium">{row.displayName}</Text>
        <Spacer />
        <Text size="xs" tone="soft">v{row.current} → v{row.latest}</Text>
      </HStack>
      {#if row.escalatesToUnsafe}
        <HStack align="center" gap={2}>
          <Chip text={t('plugins.network.unrestricted')} color="chip-red" size="small" />
          <Text size="xs" tone="danger">{t('plugins.update.requests_unrestricted')}</Text>
        </HStack>
      {/if}
      {#if row.added.length > 0}
        {#each row.added as host}
          <Text size="xs" mono>+ {host}</Text>
        {/each}
      {/if}
      {#if row.removed.length > 0}
        {#each row.removed as host}
          <Text size="xs" mono tone="soft">− {host}</Text>
        {/each}
      {/if}
      {#if !hasPermissionChanges(row)}
        <Text size="xs" tone="soft">{t('plugins.update.permissions_unchanged')}</Text>
      {/if}
    </VStack>
  {/each}
</VStack>
