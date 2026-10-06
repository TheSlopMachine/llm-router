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
    installLabel = t('plugins.install'),
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
    onaction?: (id: string, anchor?: HTMLElement) => void
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
          <Chip text={t('plugins.network.unrestricted')} color="chip-red" size="small" />
        {/if}
      </HStack>
      <HStack align="center" gap={1}>
        <Text size="xs" tone="soft">{origin}</Text>
        <Button
          style="text"
          icon={{ name: 'info' }}
          size="small"
          onclick={onDetails}
          ariaLabel={t('common.labels.details')}
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
            title={t('plugins.update_from_file')}
            ariaLabel={t('plugins.update_from_file')}
            onclick={(e) => onaction?.('update_file', e.currentTarget as HTMLElement)}
          />
        {/if}
        {#if hasUpdate}
          <Button
            style="none"
            size="medium"
            icon={{ name: 'upgrade' }}
            title={t('common.actions.update')}
            ariaLabel={t('common.actions.update')}
            onclick={(e) => onaction?.('update', e.currentTarget as HTMLElement)}
          />
        {/if}
        <Button
          style="text"
          size="medium"
          tint="var(--color-danger)"
          icon={{ name: 'delete' }}
          title={t('common.actions.delete')}
          ariaLabel={t('common.actions.delete')}
          onclick={(e) => onaction?.('delete', e.currentTarget as HTMLElement)}
        />
        <Switch
          bind:checked={enabled}
          ariaLabel={t('plugins.toggle_active')}
        />
      {:else}
        <Button
          style="none"
          icon={{ name: 'download' }}
          text={installing ? t('plugins.installing') : installLabel}
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
