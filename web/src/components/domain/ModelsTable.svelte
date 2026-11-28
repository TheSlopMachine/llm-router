<script module lang="ts">
  import type { Snippet } from 'svelte'

  export interface ModelsTableModel {
    kind: 'model' | 'virtual'
    id: string
    fullId?: string
    name?: string
    description?: string
    providerId?: string
    providerName?: string
    contextWindow?: number
    maxTokens?: number
    inputModalities?: string[]
    outputModalities?: string[]
    capabilities?: string[]
    disabled?: boolean
    custom?: boolean
    source?: unknown
  }

  export type ModelsTableActions = Snippet<[{ model: ModelsTableModel }]>
</script>

<script lang="ts">
  import { Table, type TableColumn, type TableSortDir, Chip, Icon, VStack, HStack, Text, CopyButton } from '@fuckyouryan/fui'
  import { isSortActive } from '$lib/ui-helpers'
  import { CAPABILITY_META, hasModalities, hasCapabilities } from '../../lib/capabilities'
  import { resolveModelId } from '../../lib/token-scope'
  import ModalitiesFlow from './ModalitiesFlow.svelte'
  import { t } from '../../lib/i18n.svelte'

  let {
    models,
    readonly = false,
    sortable = false,
    draggable = false,
    showProviderLink = false,
    loading = false,
    skeletonRows = 5,
    onreorder,
    actions,
    select,
    status,
    statusValue,
    empty,
    lazy = false,
    maxHeight,
  }: {
    models: ModelsTableModel[]
    readonly?: boolean
    sortable?: boolean
    draggable?: boolean
    showProviderLink?: boolean
    loading?: boolean
    skeletonRows?: number
    /** only the rows in view exist; the table scrolls inside maxHeight, or inside the height left in its column without it */
    lazy?: boolean
    maxHeight?: string
    onreorder?: (from: number, to: number) => void
    actions?: ModelsTableActions
    /** leading selection cell (a checkbox) */
    select?: ModelsTableActions
    /** status cell (last check); sorted by statusValue */
    status?: ModelsTableActions
    statusValue?: (model: ModelsTableModel) => number | string
    empty?: Snippet<[]>
  } = $props()

  // A lazy table measures its first row: every row holds the same lines (longer texts truncate,
  // the chip grids hold two rows at most).
  const windowing = $derived(
    !lazy ? {} : maxHeight !== undefined
      ? { lazy: true as const, maxHeight }
      : { lazy: true as const, grow: true as const },
  )

  let sortKey = $state<string | null>(null)
  let sortDir = $state<TableSortDir | null>(null)

  // Responsive merge stages: when narrow, context folds into modalities
  // first, then everything folds into one column. The Table measures its
  // width; it only sees the resulting column set.
  // null until the first layout
  let tableWidth = $state<number | null>(null)
  const mode = $derived.by((): 'full' | 'merged' | 'compact' => {
    const w = tableWidth
    if (w == null) return 'full'
    if (w < 640) return 'compact'
    if (w < 900) return 'merged'
    return 'full'
  })

  function modeColumns(mode: 'full' | 'merged' | 'compact', readonly: boolean, sortable: boolean): TableColumn[] {
    if (mode === 'full') {
      return [
        { key: 'context', title: t('models.context.title'), width: '0.9fr', sortable },
        { key: 'modalities', title: t('models.modalities.title'), width: '1.2fr', sortable, align: 'center' as const },
        { key: 'capabilities', title: t('models.capabilities.title'), width: readonly ? '1.1fr' : '1.2fr', sortable, align: 'left' as const },
      ]
    }
    if (mode === 'merged') {
      return [{ key: 'ctxmods', title: t('models.context_modalities'), width: '2.1fr', sortable }]
    }
    return [{ key: 'all', title: t('models.capabilities.title'), width: '2.5fr' }]
  }

  const columns = $derived<TableColumn[]>([
    ...(select && mode !== 'compact' ? [{ key: 'select', title: '', width: 'auto' }] : []),
    { key: 'model', title: t('models.list.title'), width: readonly ? '3.6fr' : '3.2fr', sortable },
    ...modeColumns(mode, readonly, sortable),
    ...(status && mode !== 'compact' ? [{ key: 'status', title: t('models.status.title'), width: '1.2fr', minWidth: 'var(--fui-table-col-xl)', sortable }] : []),
    ...(!readonly ? [{ key: 'actions', title: '', width: '0.9fr', minWidth: 'var(--fui-table-col-md)', align: 'right' as const }] : []),
  ])

  const sortedModels = $derived.by(() => {
    const key = sortKey
    const dir = sortDir
    if (!isSortActive(sortable, key, dir) || !key || !dir) return models

    const next = [...models]
    next.sort((a, b) => {
      const av = sortValue(a, key)
      const bv = sortValue(b, key)
      const result = compare(av, bv)
      return dir === 'asc' ? result : -result
    })
    return next
  })

  function sortValue(model: ModelsTableModel, key: string): number | string {
    switch (key) {
      case 'model':
        return model.kind === 'virtual' ? model.id : (model.name || model.id)
      case 'context':
      case 'ctxmods':
        return model.contextWindow ?? 0
      case 'modalities':
        return `${model.inputModalities?.join(',') ?? ''}|${model.outputModalities?.join(',') ?? ''}`
      case 'capabilities':
        return (model.capabilities ?? []).join(',')
      case 'status':
        return statusValue ? statusValue(model) : ''
      default:
        return ''
    }
  }

  function compare(a: number | string, b: number | string): number {
    if (typeof a === 'number' && typeof b === 'number') return a - b
    return String(a).localeCompare(String(b), undefined, { sensitivity: 'base', numeric: true })
  }

  function handleSort(key: string): void {
    if (!sortable) return
    if (sortKey === key) {
      if (sortDir === 'asc') sortDir = 'desc'
      else if (sortDir === 'desc') {
        sortKey = null
        sortDir = null
      } else {
        sortDir = 'asc'
      }
    } else {
      sortKey = key
      sortDir = 'asc'
    }
  }

  function fullId(model: ModelsTableModel): string {
    return resolveModelId(model)
  }

  function showProviderNameLink(model: ModelsTableModel): boolean {
    return showProviderLink && !!model.providerId && !!model.providerName
  }
</script>

{#snippet ctxBlock({ model }: { model: ModelsTableModel })}
  <!-- one line, "context / output"; the column title names the pair, the tooltip spells it out -->
  {#if model.contextWindow || model.maxTokens}
    <Text
      size="sm"
      foregroundStyle="secondary"
      class="ctx-text"
      title={`${t('models.context.window_up_to')} ${(model.contextWindow ?? 0).toLocaleString()} ${t('models.context.input_tokens')} · ${t('models.output.max_up_to')} ${(model.maxTokens ?? 0).toLocaleString()} ${t('models.output.tokens_per_response')}`}
    >
      {model.contextWindow ? `${(model.contextWindow / 1000).toFixed(0)}k` : '—'} / {model.maxTokens ? `${(model.maxTokens / 1000).toFixed(0)}k` : '—'}
    </Text>
  {:else}
    <Text size="sm" foregroundStyle="tertiary">—</Text>
  {/if}
{/snippet}

{#snippet modsBlock({ model }: { model: ModelsTableModel }, size: 'sm' | 'lg' = 'sm', direction: 'vertical' | 'horizontal' = 'vertical')}
  <ModalitiesFlow
    modalities={{ input: model.inputModalities, output: model.outputModalities }}
    chipsDirection={direction}
    size={size}
  />
  {#if !hasModalities(model)}
    <Text size="sm" foregroundStyle="tertiary">—</Text>
  {/if}
{/snippet}

{#snippet capsBlock({ model }: { model: ModelsTableModel }, size: 'sm' | 'lg' = 'sm')}
  <div class="cap-grid">
    {#each model.capabilities ?? [] as cap}
      {@const capability = CAPABILITY_META[cap]}
      <Chip
        icon={capability?.icon ?? 'help_outline'}
        text=""
        tint={capability?.chipTint ?? 'neutral'}
        title={capability ? t(capability.hint) : cap}
        size={size}
      />
    {/each}
    {#if model.custom}
      <Chip icon="tune" text="" tint="teal" title={t('models.custom.lowercase')} size={size} />
    {/if}
    {#if !hasCapabilities(model)}
      <Text size="sm" foregroundStyle="tertiary">—</Text>
    {/if}
  </div>
{/snippet}

<Table
    bind:measuredWidth={tableWidth}
    {...windowing}
    {columns}
    rows={sortedModels}
    rowKey={(model) => (model as ModelsTableModel).fullId ?? `${(model as ModelsTableModel).kind}:${(model as ModelsTableModel).id}`}
    {loading}
    {skeletonRows}
    {draggable}
    onreorder={onreorder}
    {sortKey}
    {sortDir}
    onsort={handleSort}
    rowClass={(model) => ((model as ModelsTableModel).disabled ? 'row-off' : '')}
    empty={empty}
  >
    {#snippet cell({ column, row })}
      {@const model = row as ModelsTableModel}
      {#if column.key === 'model'}
        {@const lineLimit = lazy ? 1 : undefined}
        <VStack spacing="tight">
          {#if model.kind === 'virtual'}
            <HStack spacing="tight">
              <Text mono size="sm" {lineLimit} text={`virtual/${model.id}`} />
              <CopyButton text={fullId(model)} title={t('models.actions.copy_id')} ariaLabel={t('models.actions.copy_id')} />
            </HStack>
            <Text size="sm" foregroundStyle="secondary" {lineLimit}>{model.description || '—'}</Text>
          {:else}
            <Text size="md" weight="medium" {lineLimit}>{model.name || model.id}</Text>
            <HStack spacing="tight">
              <Text mono size="sm" {lineLimit}>{fullId(model)}</Text>
              <CopyButton text={fullId(model)} title={t('models.actions.copy_id')} ariaLabel={t('models.actions.copy_id')} />
            </HStack>
          {/if}
          {#if showProviderNameLink(model)}
            <HStack spacing="none">
              <a class="provider-link" href={`#/providers/${model.providerId}`}>
                <Text size="sm" {lineLimit}>{model.providerName}</Text>
                <Icon name="chevron_right" size="sm" />
              </a>
            </HStack>
          {/if}
        </VStack>
      {:else if column.key === 'context'}
        {@render ctxBlock({ model })}
      {:else if column.key === 'modalities'}
        {@render modsBlock({ model })}
      {:else if column.key === 'capabilities'}
        {@render capsBlock({ model })}
      {:else if column.key === 'ctxmods'}
        <VStack spacing="tight" halign="start">
          {@render ctxBlock({ model })}
          {@render modsBlock({ model }, 'sm', 'horizontal')}
        </VStack>
      {:else if column.key === 'all'}
        <VStack halign="start">
          {@render ctxBlock({ model })}
          {@render modsBlock({ model }, 'sm', 'horizontal')}
          {@render capsBlock({ model }, 'sm')}
        </VStack>
      {:else if column.key === 'actions'}
        <HStack halign="end">
          {#if actions}{@render actions({ model })}{/if}
        </HStack>
      {:else if column.key === 'select'}
        {#if select}{@render select({ model })}{/if}
      {:else if column.key === 'status'}
        {#if status}{@render status({ model })}{/if}
      {/if}
    {/snippet}
    {#snippet card({ row })}
      {@const model = row as ModelsTableModel}
      <HStack valign="center">
        {#if select}{@render select({ model })}{/if}
        <VStack spacing="tight" grow>
          <Text variant="value">{model.kind === 'virtual' ? `virtual/${model.id}` : model.name || model.id}</Text>
          {#if model.kind === 'virtual'}
            <Text variant="caption">{model.description || '—'}</Text>
          {:else}
            <Text variant="caption" mono>{fullId(model)}</Text>
          {/if}
        </VStack>
        {#if actions}<HStack>{@render actions({ model })}</HStack>{/if}
      </HStack>
      <HStack wrap>
        {#if model.contextWindow || model.maxTokens}<Text variant="caption">{model.contextWindow ? `${(model.contextWindow / 1000).toFixed(0)}k` : '—'} / {model.maxTokens ? `${(model.maxTokens / 1000).toFixed(0)}k` : '—'}</Text>{/if}
        {#if (model.capabilities ?? []).length > 0}<Text variant="caption">{(model.capabilities ?? []).join(', ')}</Text>{/if}
      </HStack>
      {#if status}{@render status({ model })}{/if}
    {/snippet}
  </Table>

<style>
  /* two rows at most; more chips add columns */
  .cap-grid {
    display: grid;
    grid-auto-flow: column;
    grid-template-rows: repeat(2, auto);
    gap: var(--fui-spacing-tight);
    justify-content: start;
    align-items: center;
  }
  /* in-app navigation row, not a text link: no underline, the cell's text colour */
  .provider-link {
    display: inline-flex;
    align-items: center;
    max-width: 100%;
    min-width: 0;
    color: inherit;
    text-decoration: none;
  }
</style>
