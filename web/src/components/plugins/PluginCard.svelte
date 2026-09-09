<script lang="ts">
  import type { Snippet } from 'svelte'
  import ActionDropdown from '../ActionDropdown.svelte'

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
    subtitle = '',
    description = '',
    typeKeys = [],
    mode,
    actions = [],
    installLabel = 'Install',
    installing = false,
    onInstall,
    onaction,
    children
  } = $props<{
    title: string
    meta?: string
    badges?: PluginBadge[]
    subtitle?: string
    description?: string
    typeKeys?: string[]
    mode: 'installed' | 'uninstalled'
    actions?: PluginCardAction[]
    installLabel?: string
    installing?: boolean
    onInstall?: () => void
    onaction?: (id: string) => void
    children?: Snippet
  }>()
</script>

<div class="card">
  <div class="plugin-header">
    <div>
      <strong>{title}</strong>
      {#if meta}<span class="muted-inline">{meta}</span>{/if}
      {#each badges as badge}
        <span class="badge {badge.kind}">{badge.text}</span>
      {/each}
    </div>
    <div class="plugin-actions">
      {#if mode === 'uninstalled'}
        <button class="btn btn-secondary" disabled={installing} onclick={() => onInstall?.()}>
          {installing ? 'Installing…' : installLabel}
        </button>
      {:else}
        <ActionDropdown {actions} label="Actions" rounded="lg" onaction={(id) => onaction?.(id)} />
      {/if}
    </div>
  </div>
  {#if subtitle}<div class="muted">{subtitle}</div>{/if}
  {#if description}<div class="muted">{description}</div>{/if}
  {#if typeKeys.length > 0}
    <div class="type-keys">
      {#each typeKeys as key}
        <span class="badge">{key}</span>
      {/each}
    </div>
  {/if}
  {#if children}{@render children()}{/if}
</div>

<style>
  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 16px;
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
  .type-keys {
    display: flex;
    gap: 6px;
    margin-top: 8px;
    flex-wrap: wrap;
  }
  .badge {
    margin-left: 6px;
  }
  .type-keys .badge {
    margin-left: 0;
  }
</style>
