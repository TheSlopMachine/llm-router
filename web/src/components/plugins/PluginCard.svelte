<script lang="ts">
  import ActionDropdown from '../ActionDropdown.svelte'
  import { squircle } from '../../lib/squircle'

  export interface PluginBadge {
    text: string
    kind: '' | 'badge-red' | 'badge-green' | 'badge-blue' | 'badge-yellow'
  }

  export interface PluginCardAction {
    id: string
    label: string
    icon?: string
    disabled?: boolean
    danger?: boolean
  }

  let {
    title,
    meta = '',
    badges = [],
    description = '',
    mode,
    onDetails,
    actions = [],
    onaction,
    installLabel = 'Install',
    installing = false,
    onInstall
  } = $props<{
    title: string
    meta?: string
    badges?: PluginBadge[]
    description?: string
    mode: 'installed' | 'uninstalled'
    onDetails?: () => void
    actions?: PluginCardAction[]
    onaction?: (id: string) => void
    installLabel?: string
    installing?: boolean
    onInstall?: () => void
  }>()
</script>

<div class="card" use:squircle={18}>
  <div class="plugin-header">
    <div>
      <strong>{title}</strong>
      {#if meta}<span class="muted-inline">{meta}</span>{/if}
      {#each badges as badge}
        <span class="badge {badge.kind}">{badge.text}</span>
      {/each}
    </div>
    <div class="plugin-actions">
      {#if mode === 'installed'}
        <ActionDropdown {actions} label="Actions" rounded="lg" onaction={(id) => onaction?.(id)} />
        <button class="btn-icon" onclick={() => onDetails?.()} aria-label="Details">
          <span class="icon">info</span>
        </button>
      {:else}
        <button class="btn btn-secondary" disabled={installing} onclick={() => onInstall?.()}>
          {installing ? 'Installing…' : installLabel}
        </button>
        <button class="btn-icon" onclick={() => onDetails?.()} aria-label="Details">
          <span class="icon">info</span>
        </button>
      {/if}
    </div>
  </div>
  {#if description}<div class="muted">{description}</div>{/if}
</div>

<style>
  .card {
    background: var(--color-surface-container-high);
    border-radius: var(--radius-lg);
    padding: 20px;
  }
  .plugin-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
  }
  .plugin-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    align-items: center;
  }
  .muted-inline {
    font-size: 13px;
    color: var(--color-text-soft);
    margin-left: 8px;
  }
  .muted {
    font-size: 13px;
    color: var(--color-text-soft);
    margin-top: 4px;
  }
  .badge {
    margin-left: 6px;
  }
</style>
