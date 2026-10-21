<script lang="ts">
  import { Button, FloatingView, HStack, VStack, Text, Chip, Spacer, Toggle } from '@fuckyouryan/fui'
  import InstallConfirm from './InstallConfirm.svelte'
  import { t } from '$lib/i18n.svelte'
  import { viewport } from '$lib/viewport.svelte'

  let {
    title,
    version,
    description = '',
    mode,
    unsafe = false,
    disabled = false,
    toggling = false,
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
    onToggle,
    onaction
  }: {
    title: string
    version: string
    description?: string
    mode: 'installed' | 'uninstalled'
    unsafe?: boolean
    disabled?: boolean
    toggling?: boolean
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
    onToggle?: (enabled: boolean) => void
    onaction?: (id: string, anchor?: HTMLElement) => void
  } = $props()

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
  <HStack valign="center" gap={4} wrap>
    <HStack valign="center" gap={2} wrap class="plugin-title">
      <span class="plugin-name"><Text variant="value">{title}</Text></span>
      <Chip text={version} tint="accent" size="sm" />
      {#if unsafe}
        <Chip text={t('plugins.network.unrestricted')} tint="danger" size="sm" />
      {/if}
    </HStack>

    <HStack valign="center" gap={2}>
      {#if mode === 'installed'}
        {#if isManual}
          <Button
            buttonStyle="neutral"
            size="md"
            icon={{ name: 'upload_file' }}
            title={t('plugins.update_from_file')}
            ariaLabel={t('plugins.update_from_file')}
            onclick={(e) => onaction?.('update_file', e.currentTarget as HTMLElement)}
          />
        {/if}
        {#if hasUpdate}
          <Button
            buttonStyle="neutral"
            size="md"
            icon={{ name: 'upgrade' }}
            title={t('common.actions.update')}
            ariaLabel={t('common.actions.update')}
            onclick={(e) => openConfirm(e.currentTarget as HTMLElement)}
          />
        {/if}
        <Button
          buttonStyle="text"
          size="md"
          tint="var(--fui-color-danger)"
          icon={{ name: 'delete' }}
          title={t('common.actions.delete')}
          ariaLabel={t('common.actions.delete')}
          onclick={(e) => onaction?.('delete', e.currentTarget as HTMLElement)}
        />
        <Toggle
          checked={!disabled}
          disabled={toggling}
          ariaLabel={t('plugins.toggle_active')}
          onchange={(next) => onToggle?.(next)}
        />
      {:else}
        <Button
          buttonStyle="neutral"
          icon={{ name: 'download' }}
          text={viewport.narrow ? '' : (installing ? t('plugins.installing') : installLabel)}
          disabled={installing}
          onclick={(e) => openConfirm(e.currentTarget as HTMLElement)}
        />
      {/if}
    </HStack>
  </HStack>

  {#if description}
    <Text variant="subtitle">{description}</Text>
  {/if}

  <FloatingView
    open={confirmOpen}
    anchor={confirmAnchor}
    width="md"
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
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-1);
  }
  .plugin-card-row :global(.plugin-title) {
    flex: 1 1 var(--fui-grid-min-lg);
    min-width: 0;
  }
  .plugin-name {
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
