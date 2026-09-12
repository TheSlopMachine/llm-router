<script lang="ts">
  import type { Snippet } from 'svelte'
  import { t } from '../../../lib/i18n.svelte'

  type Group = {
    id: string
    title: string
    subtitle?: string
    items: any[]
    error?: string
  }

  let {
    groups,
    selected,
    query = '',
    disabled = false,
    emptyLabel = 'No items',
    noMatchLabel = 'No matches',
    getItemId,
    matchItem,
    onToggle,
    onSelectAll,
    row
  } = $props<{
    groups: Group[]
    selected: Set<string>
    query?: string
    disabled?: boolean
    emptyLabel?: string
    noMatchLabel?: string
    getItemId: (group: Group, item: any) => string
    matchItem?: (item: any, q: string) => boolean
    onToggle: (id: string) => void
    onSelectAll?: (ids: string[], select: boolean) => void
    row?: Snippet<[{ item: any; id: string; checked: boolean; group: Group }]>
  }>()

  function defaultMatch(item: any, q: string): boolean {
    return String(item).toLowerCase().includes(q.toLowerCase())
  }

  function isMatch(item: any, q: string): boolean {
    if (!q.trim()) return true
    return (matchItem ?? defaultMatch)(item, q)
  }

  function filtered(group: Group, q: string): any[] {
    if (!q.trim()) return group.items
    return group.items.filter((it: any) => isMatch(it, q))
  }

  function isGroupAllSelected(group: Group, q: string): boolean {
    const f = filtered(group, q)
    if (f.length === 0) return false
    return f.every((it: any) => selected.has(getItemId(group, it)))
  }

  function groupSelectedCount(group: Group): number {
    let c = 0
    for (const it of group.items) if (selected.has(getItemId(group, it))) c++
    return c
  }

  function selectAll(group: Group, select: boolean, q: string): void {
    const f = filtered(group, q)
    const ids = f.map((it: any) => getItemId(group, it))
    if (onSelectAll) {
      onSelectAll(ids, select)
      return
    }
    for (const id of ids) {
      const has = selected.has(id)
      if (select && !has) onToggle(id)
      if (!select && has) onToggle(id)
    }
  }
</script>

<div class="model-sections" class:is-disabled={disabled}>
  {#each groups as group}
    <div class="model-section">
      <div class="section-header">
        <span class="section-title">{group.title}</span>
        {#if group.subtitle}
          <span class="text-muted" style="font-size: 12px;">{group.subtitle}</span>
        {/if}
        <span class="section-count">{groupSelectedCount(group)} / {group.items.length}</span>
        <button
          type="button"
          class="btn-link select-all-btn"
          onclick={() => selectAll(group, !isGroupAllSelected(group, query), query)}
          disabled={disabled || group.items.length === 0}
        >
          {isGroupAllSelected(group, query) ? t('Deselect all') : t('Select all')}
        </button>
        {#if group.error}
          <span class="badge badge-red">{group.error}</span>
        {/if}
      </div>

      {#if group.items.length}
        {@const f = filtered(group, query)}
        {#if f.length === 0}
          <div class="muted-placeholder">{t(noMatchLabel)} for "{query}"</div>
        {:else}
          <div class="checkbox-list" class:cred-list={group.items[0]?.label !== undefined}>
            {#each f as item}
              {@const id = getItemId(group, item)}
              {@const checked = selected.has(id)}
              {#if row}
                {@render row({ item, id, checked, group })}
              {:else}
                <label class="checkbox-item">
                  <input type="checkbox" {checked} onchange={() => onToggle(id)} {disabled} />
                  <span class="mono">{String(item)}</span>
                </label>
              {/if}
            {/each}
          </div>
        {/if}
      {:else if !group.error}
        <div class="muted-placeholder">{t(emptyLabel)}</div>
      {/if}
    </div>
  {/each}

  {#if groups.length === 0}
    <div class="muted-placeholder">{t(emptyLabel)}</div>
  {/if}
</div>

<style>
  .model-sections {
    display: flex;
    flex-direction: column;
    gap: 24px;
    margin-top: 16px;
  }
  .model-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .section-header {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .section-title {
    font-size: 13px;
    font-weight: 500;
    color: var(--color-text);
  }
  .section-count {
    font-size: 12px;
    color: var(--color-text-soft);
  }
  .select-all-btn {
    font-size: 12px;
    padding: 0 8px;
    height: 24px;
  }
  .checkbox-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 8px 16px;
    margin-top: 8px;
  }
  .checkbox-list.cred-list {
    grid-template-columns: 1fr;
  }
  .checkbox-item {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    cursor: pointer;
    user-select: none;
  }
  .checkbox-item input[type='checkbox'] {
    width: auto;
    cursor: pointer;
  }
</style>
