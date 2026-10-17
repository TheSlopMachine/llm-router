<script lang="ts">
  import { Button, FloatingView, HStack, VStack, Text, Chip, Spacer, Switch } from '@fuckyouryan/fui'
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
  } = $props<{
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
  }>()

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
  <HStack align="center" gap={4} wrap>
    <HStack align="center" gap={2} wrap class="plugin-title">
      <span class="plugin-name"><Text variant="value">{title}</Text></span>
      <Chip text={version} color="chip-accent" size="small" />
      {#if unsafe}
        <Chip text={t('plugins.network.unrestricted')} color="chip-red" size="small" />
      {/if}
    </HStack>

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
          checked={!disabled}
          disabled={toggling}
          ariaLabel={t('plugins.toggle_active')}
          onchange={(next) => onToggle?.(next)}
        />
      {:else}
        <Button
          style="none"
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
