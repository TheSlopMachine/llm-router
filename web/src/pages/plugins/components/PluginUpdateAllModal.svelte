<script lang="ts">
  import { VStack, HStack, Text, Chip, Spacer } from '$ui'
  import { t } from '$lib/i18n.svelte'
  import type { UpdateAllRow } from './install-confirm'

  let { rows } = $props<{ rows: UpdateAllRow[] }>()
</script>

<VStack gap={4}>
  <Text size="sm" tone="soft">{t('Review host changes before updating.')}</Text>
  {#each rows as row (row.pluginId)}
    <VStack gap={1}>
      <HStack align="center" gap={2}>
        <Text size="sm" weight="medium">{row.displayName}</Text>
        <Spacer />
        <Text size="xs" tone="soft">v{row.current} → v{row.latest}</Text>
      </HStack>
      {#if row.escalatesToUnsafe}
        <HStack align="center" gap={2}>
          <Chip text={t('Unrestricted network')} color="chip-red" size="small" />
          <Text size="xs" tone="danger">{t('Requests unrestricted network access.')}</Text>
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
      {#if row.added.length === 0 && row.removed.length === 0 && !row.escalatesToUnsafe}
        <Text size="xs" tone="soft">{t('Permissions unchanged.')}</Text>
      {/if}
    </VStack>
  {/each}
</VStack>
