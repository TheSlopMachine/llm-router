<script lang="ts">
  import { Button, Chip, DeleteButton, FloatingView, ListRow, Toggle } from '@fuckyouryan/fui'
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
    updateVersion,
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
    /** the version an available update installs ("v1.2.0"); unset when there is none */
    updateVersion?: string
    installing?: boolean
    installLabel?: string
    onInstall?: () => void
    onUpdate?: () => void
    /** resolves false when the plugin keeps its state */
    onToggle?: (enabled: boolean) => Promise<boolean>
    onaction?: (id: string, anchor: HTMLElement) => void
  } = $props()

  let confirmAnchor = $state<HTMLElement>()
  let confirmOpen = $state(false)
  // The switch flips on click; a refused change puts it back to the stored state.
  let enabled = $derived(!disabled)

  async function toggle(next: boolean): Promise<void> {
    if (!(await onToggle?.(next))) enabled = !disabled
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

<ListRow {title} subtitle={description || undefined} image={iconUrl} icon={subsystemIcon(subsystems)}>
    {#snippet accessories()}
      <Chip text={updateVersion ? `${version} → ${updateVersion}` : version} tint="accent" size="sm" />
      {#if permissions?.unsafe}
        <Chip text={t('plugins.network.unrestricted')} tint="danger" size="sm" />
      {/if}
      {#if permissions?.docker}
        <Chip text={t('plugins.docker.label')} tint="warning" size="sm" />
      {/if}
    {/snippet}
    {#snippet trailing()}
      {#if mode === 'installed'}
        {#if isManual}
          <Button buttonStyle="neutral" size="sm" icon={{ name: 'upload_file' }} title={t('plugins.update_from_file')} ariaLabel={t('plugins.update_from_file')}
            onclick={(e) => onaction?.('update_file', e.currentTarget as HTMLElement)} />
        {/if}
        {#if updateVersion}
          <Button buttonStyle="neutral" size="sm" icon={{ name: 'upgrade' }} title={t('common.actions.update')} ariaLabel={t('common.actions.update')}
            onclick={(e) => openConfirm(e.currentTarget as HTMLElement)} />
        {/if}
        <DeleteButton size="sm" onclick={(e) => onaction?.('delete', e.currentTarget as HTMLElement)} />
        <Toggle bind:checked={enabled} disabled={toggling} ariaLabel={t('plugins.toggle_active')} onchange={(next) => void toggle(next)} />
      {:else}
        <Button
          buttonStyle="neutral"
          icon={{ name: 'download' }}
          text={viewport.narrow ? '' : (installing ? t('plugins.installing') : installLabel)}
          disabled={installing || !review}
          onclick={(e) => openConfirm(e.currentTarget as HTMLElement)}
        />
      {/if}
    {/snippet}
  </ListRow>

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

