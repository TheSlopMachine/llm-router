<script lang="ts">
  import { Button, FloatingView, HStack, VStack, Text, Chip, Spacer, Switch } from '$ui'
  import InstallConfirm from './InstallConfirm.svelte'
  import { t } from '$lib/i18n.svelte'

  let {
    title,
    version,
    description = '',
    mode,
    unsafe = false,
    isManual = false,
    hasUpdate = false,
    installing = false,
    installLabel = t('plugins.install'),
    allowHosts = [],
    newHosts = [],
    added = [],
    removed = [],
    escalatesToUnsafe = false,
    latestVersion = '',
    onInstall,
    onUpdate,
    onaction
  } = $props<{
    title: string
    version: string
    description?: string
    mode: 'installed' | 'uninstalled'
    unsafe?: boolean
    isManual?: boolean
    hasUpdate?: boolean
    installing?: boolean
    installLabel?: string
    allowHosts?: string[]
    newHosts?: string[]
    added?: string[]
    removed?: string[]
    escalatesToUnsafe?: boolean
    latestVersion?: string
    onInstall?: () => void
    onUpdate?: () => void
    onaction?: (id: string, anchor?: HTMLElement) => void
  }>()

  let enabled = $state(true)
  let confirmAnchor = $state<HTMLElement>()
  let confirmOpen = $state(false)

  function openConfirm(anchor: HTMLElement): void {
    confirmAnchor = anchor
    confirmOpen = true
  }

  function handleConfirm(): void {
    confirmOpen = false
    if (mode === 'installed') onUpdate?.()
    else onInstall?.()
  }
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
            onclick={(e) => openConfirm(e.currentTarget as HTMLElement)}
          />
        {/if}
        <Button
          style="text"
          size="medium"
          tint="var(--fui-color-danger)"
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
          onclick={(e) => openConfirm(e.currentTarget as HTMLElement)}
        />
      {/if}
    </HStack>
  </HStack>

  <FloatingView
    open={confirmOpen}
    anchor={confirmAnchor}
    onclose={() => { confirmOpen = false }}
    label={mode === 'installed' ? t('common.actions.update') : installLabel}
  >
    {#snippet children({ close })}
      <InstallConfirm
        allowHosts={mode === 'installed' && newHosts.length > 0 ? newHosts : allowHosts}
        unsafe={unsafe}
        added={added}
        removed={removed}
        escalatesToUnsafe={escalatesToUnsafe}
        confirmLabel={mode === 'installed' ? t('common.actions.update') : installLabel}
        busy={installing}
        onCancel={close}
        onConfirm={handleConfirm}
      />
    {/snippet}
  </FloatingView>
</div>

<style>
  .plugin-card-row {
    padding: var(--fui-space-4);
  }
</style>
