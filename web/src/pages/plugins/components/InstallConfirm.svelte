<script lang="ts">
  import { Button, Chip, HStack, Text, VStack } from '$ui'
  import { t } from '$lib/i18n.svelte'

  let {
    allowHosts,
    unsafe,
    added = [],
    removed = [],
    escalatesToUnsafe = false,
    confirmLabel,
    busy = false,
    onCancel,
    onConfirm
  } = $props<{
    allowHosts: string[]
    unsafe: boolean
    added?: string[]
    removed?: string[]
    escalatesToUnsafe?: boolean
    confirmLabel: string
    busy?: boolean
    onCancel: () => void
    onConfirm: () => void
  }>()

  let hasChanges = $derived(added.length > 0 || removed.length > 0 || escalatesToUnsafe)
</script>

<VStack gap={3} style="max-width: 320px;">
  <VStack gap={2}>
    <Text tag="h3" size="sm" weight="bold">{t('plugins.permissions_requested')}</Text>
    {#if unsafe || escalatesToUnsafe}
      <div><Chip text={t('plugins.network.unrestricted')} color="chip-red" size="small" /></div>
    {/if}
    {#if !hasChanges}
      {#if !unsafe && allowHosts.length === 0}
        <Text size="sm" tone="soft">{t('plugins.network.none')}</Text>
      {:else if !unsafe}
        <VStack gap={1}>
          {#each allowHosts as host}
            <Text size="xs" mono tone="soft">{host}</Text>
          {/each}
        </VStack>
      {/if}
    {:else}
      {#if added.length > 0}
        <VStack gap={1}>
          {#each added as host}
            <Text size="xs" mono>+ {host}</Text>
          {/each}
        </VStack>
      {/if}
      {#if removed.length > 0}
        <VStack gap={1}>
          {#each removed as host}
            <Text size="xs" mono tone="soft">− {host}</Text>
          {/each}
        </VStack>
      {/if}
      {#if !escalatesToUnsafe && allowHosts.length > 0}
        <VStack gap={1}>
          {#each allowHosts as host}
            <Text size="xs" mono tone="soft">{host}</Text>
          {/each}
        </VStack>
      {/if}
    {/if}
  </VStack>

  <HStack justify="end" gap={3}>
    <Button size="small" style="text" onclick={onCancel} disabled={busy}>{t('common.actions.cancel')}</Button>
    <Button size="small" style="prominent" onclick={onConfirm} disabled={busy}>
      {confirmLabel}
    </Button>
  </HStack>
</VStack>
