<script lang="ts">
  import { Button, FloatingView, HStack, VStack, Text, Chip, Spacer, Toggle, Image } from '@fuckyouryan/fui'
  import InstallConfirm from './InstallConfirm.svelte'
  import { t } from '$lib/i18n.svelte'
  import { viewport } from '$lib/viewport.svelte'
  import { subsystemIcon } from '$lib/subsystems'
  import type { PermissionDiff } from '$lib/plugin-permissions'
  import type { PluginPermissions } from '$lib/types'

  let {
    title,
    version,
    description = '',
    iconUrl = '',
    subsystems = [],
    mode,
    permissions,
    review,
    diff = null,
    disabled = false,
    toggling = false,
    isManual = false,
    hasUpdate = false,
    installing = false,
    installLabel = t('plugins.install'),
    onInstall,
    onUpdate,
    onToggle,
    onaction
  }: {
    title: string
    version: string
    description?: string
    iconUrl?: string
    subsystems?: string[]
    mode: 'installed' | 'uninstalled'
    /** what the plugin holds now, or requests when not installed; null when its file failed to load */
    permissions: PluginPermissions | null
    /** what installing or updating approves */
    review: PluginPermissions | null
    diff?: PermissionDiff | null
    disabled?: boolean
    toggling?: boolean
    isManual?: boolean
    hasUpdate?: boolean
    installing?: boolean
    installLabel?: string
    onInstall?: () => void
    onUpdate?: () => void
    /** resolves false when the plugin keeps its state */
    onToggle?: (enabled: boolean) => Promise<boolean>
    onaction?: (id: string, anchor?: HTMLElement) => void
  } = $props()

  let confirmAnchor = $state<HTMLElement>()
  let confirmOpen = $state(false)
  // The switch flips on click. A refused change remounts it at the stored state.
  let toggleRevision = $state(0)

  async function toggle(enabled: boolean): Promise<void> {
    if (!(await onToggle?.(enabled))) toggleRevision++
  }

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
  <Image
    src={iconUrl}
    width="var(--fui-size-icon-lg)"
    height="var(--fui-size-icon-lg)"
    fit="contain"
    radius="sm"
    fallbackIcon={subsystemIcon(subsystems)}
    style="--fui-icon-size: var(--fui-size-icon-lg); flex: none"
  />
  <div class="plugin-card-content">
  <HStack valign="center" gap={4}>
    <HStack valign="center" gap={2} wrap class="plugin-title">
      <span class="plugin-name"><Text variant="value">{title}</Text></span>
      <Chip text={version} tint="accent" size="sm" />
      {#if permissions?.unsafe}
        <Chip text={t('plugins.network.unrestricted')} tint="danger" size="sm" />
      {/if}
      {#if permissions?.docker}
        <Chip text={t('plugins.docker.label')} tint="warning" size="sm" />
      {/if}
    </HStack>

    <HStack valign="center" gap={2} class="plugin-actions">
      {#if mode === 'installed'}
        {#if isManual}
          <Button
            buttonStyle="neutral"
            size="sm"
            icon={{ name: 'upload_file' }}
            title={t('plugins.update_from_file')}
            ariaLabel={t('plugins.update_from_file')}
            onclick={(e) => onaction?.('update_file', e.currentTarget as HTMLElement)}
          />
        {/if}
        {#if hasUpdate}
          <Button
            buttonStyle="neutral"
            size="sm"
            icon={{ name: 'upgrade' }}
            title={t('common.actions.update')}
            ariaLabel={t('common.actions.update')}
            onclick={(e) => openConfirm(e.currentTarget as HTMLElement)}
          />
        {/if}
        <Button
          buttonStyle="text"
          size="sm"
          tint="var(--fui-color-danger)"
          icon={{ name: 'delete' }}
          title={t('common.actions.delete')}
          ariaLabel={t('common.actions.delete')}
          onclick={(e) => onaction?.('delete', e.currentTarget as HTMLElement)}
        />
        {#key toggleRevision}
          <Toggle
            checked={!disabled}
            disabled={toggling}
            ariaLabel={t('plugins.toggle_active')}
            onchange={(next) => void toggle(next)}
          />
        {/key}
      {:else}
        <Button
          buttonStyle="neutral"
          icon={{ name: 'download' }}
          text={viewport.narrow ? '' : (installing ? t('plugins.installing') : installLabel)}
          disabled={installing || !review}
          onclick={(e) => openConfirm(e.currentTarget as HTMLElement)}
        />
      {/if}
    </HStack>
  </HStack>

  {#if description}
    <Text variant="subtitle">{description}</Text>
  {/if}
  </div>

  <FloatingView
    open={confirmOpen}
    anchor={confirmAnchor}
    width="md"
    onclose={() => { confirmOpen = false }}
    label={mode === 'installed' ? t('common.actions.update') : installLabel}
  >
    {#snippet children({ close })}
      {#if review}
        <InstallConfirm
          permissions={review}
          {diff}
          confirmLabel={mode === 'installed' ? t('common.actions.update') : installLabel}
          busy={installing}
          oncancel={close}
          onconfirm={handleConfirm}
        />
      {/if}
    {/snippet}
  </FloatingView>
</div>

<style>
  .plugin-card-row {
    padding: var(--fui-space-3);
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--fui-space-3);
  }
  .plugin-card-content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0;
  }
  .plugin-card-row :global(.plugin-title) {
    flex: 1 1 0;
    min-width: 0;
  }
  .plugin-card-row :global(.plugin-actions) {
    flex: none;
  }
  .plugin-name {
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
