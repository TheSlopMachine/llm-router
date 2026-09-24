<script lang="ts">
  import { Button, HStack, VStack, Text, Chip, Spacer, Switch } from '$ui'
  import { t } from '$lib/i18n.svelte'

  let {
    title,
    version,
    origin,
    description = '',
    mode,
    unsafe = false,
    isManual = false,
    hasUpdate = false,
    installing = false,
    installLabel = t('Install'),
    onDetails,
    onInstall,
    onaction
  } = $props<{
    title: string
    version: string
    origin: string
    description?: string
    mode: 'installed' | 'uninstalled'
    unsafe?: boolean
    isManual?: boolean
    hasUpdate?: boolean
    installing?: boolean
    installLabel?: string
    onDetails?: () => void
    onInstall?: () => void
    onaction?: (id: string) => void
  }>()

  let enabled = $state(true)
</script>

<div class="plugin-card-row">
  <HStack align="center" gap={4}>
    <VStack gap={1} grow>
      <HStack align="center" gap={2}>
        <Text tag="h2" size="base" weight="bold">{title}</Text>
        <Chip text={version} color="chip-accent" size="small" />
        {#if unsafe}
          <Chip text={t('Unrestricted network')} color="chip-red" size="small" />
        {/if}
      </HStack>
      <HStack align="center" gap={1}>
        <Text size="xs" tone="soft">{origin}</Text>
        <Button
          style="text"
          icon={{ name: 'info' }}
          size="small"
          onclick={onDetails}
          ariaLabel={t('Details')}
        />
      </HStack>
      {#if description}
        <Text tag="h3" size="sm" tone="soft">{description}</Text>
      {/if}
    </VStack>

    <Spacer />

    <HStack align="center" gap={2}>
      {#if mode === 'installed'}
        {#if isManual}
          <Button
            style="none"
            size="medium"
            icon={{ name: 'upload_file' }}
            title={t('Update from file…')}
            ariaLabel={t('Update from file…')}
            onclick={() => onaction?.('update_file')}
          />
        {/if}
        {#if hasUpdate}
          <Button
            style="none"
            size="medium"
            icon={{ name: 'upgrade' }}
            title={t('Update')}
            ariaLabel={t('Update')}
            onclick={() => onaction?.('update')}
          />
        {/if}
        <Button
          style="text"
          size="medium"
          tint="#dc2626"
          icon={{ name: 'delete' }}
          title={t('Delete')}
          ariaLabel={t('Delete')}
          onclick={() => onaction?.('delete')}
        />
        <Switch
          bind:checked={enabled}
          ariaLabel={t('Toggle plugin active')}
        />
      {:else}
        <Button
          style="none"
          icon={{ name: 'download' }}
          text={installing ? t('Installing…') : installLabel}
          disabled={installing}
          onclick={onInstall}
        />
      {/if}
    </HStack>
  </HStack>
</div>

<style>
  .plugin-card-row {
    padding: var(--space-4);
  }
</style>
