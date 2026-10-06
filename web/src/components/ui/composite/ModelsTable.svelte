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
  import Table from './Table.svelte'
  import type { TableColumn, TableSortDir } from './Table.svelte'
  import Chip from '../controls/Chip.svelte'
  import Icon from '../controls/Icon.svelte'
  import VStack from '../layout/VStack.svelte'
  import HStack from '../layout/HStack.svelte'
  import Text from '../controls/Text.svelte'
  import { CAPABILITY_META, hasModalities, hasCapabilities } from '../../../lib/capabilities'
  import { resolveModelId } from '../../../lib/token-scope'
  import { isSortActive } from '../../../lib/table-layout'
  import ModalitiesFlow from './ModalitiesFlow.svelte'
  import CopyButton from './CopyButton.svelte'
  import { t } from '../../../lib/i18n.svelte'
  import { squircle } from '../../../lib/squircle'

  let {
    models,
    readonly = false,
    sortable = false,
    draggable = false,
    showProviderLink = false,
    loading = false,
    skeletonRows = 5,
    onReorder,
    actions,
    empty,
  } = $props<{
    models: ModelsTableModel[]
    readonly?: boolean
    sortable?: boolean
    draggable?: boolean
    showProviderLink?: boolean
    loading?: boolean
    skeletonRows?: number
    onReorder?: (from: number, to: number) => void
    actions?: ModelsTableActions
    empty?: Snippet<[]>
  }>()

  let sortKey = $state<string | null>(null)
  let sortDir = $state<TableSortDir | null>(null)

  // Responsive merge stages: when narrow, context folds into modalities
  // first, then everything folds into one column. Measured on a local
  // wrapper — Table only sees the resulting column set.
  let wrapEl = $state<HTMLElement | null>(null)
  let tableWidth = $state<number | null>(null)
  $effect(() => {
    const el = wrapEl
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      tableWidth = entries[0]?.contentRect.width ?? null
    })
    ro.observe(el)
    return () => ro.disconnect()
  })
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
    { key: 'model', title: t('models.list.title'), width: readonly ? '3.6fr' : '3.2fr', sortable },
    ...modeColumns(mode, readonly, sortable),
    ...(!readonly ? [{ key: 'actions', title: t('common.actions.all'), width: '0.9fr', align: 'right' as const }] : []),
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

  function openProvider(providerId: string): void {
    window.location.hash = `#/providers/${providerId}`
  }

  function showProviderNameLink(model: ModelsTableModel): boolean {
    return showProviderLink && !!model.providerId && !!model.providerName
  }

  function showVirtualLink(model: ModelsTableModel): boolean {
    return showProviderLink && model.kind === 'virtual'
  }
</script>

{#snippet ctxBlock({ model }: { model: ModelsTableModel })}
  <VStack gap={1} align="start" class="ctx-text">
    {#if model.contextWindow}
      <Text size="sm" tone="soft" title={t('models.context.window_up_to') + ` ${model.contextWindow.toLocaleString()} ` + t('models.context.input_tokens')}>
        {(model.contextWindow / 1000).toFixed(0)}k {t('models.context.lowercase')}
      </Text>
    {/if}
    {#if model.maxTokens}
      <Text size="sm" tone="soft" title={t('models.output.max_up_to') + ` ${model.maxTokens.toLocaleString()} ` + t('models.output.tokens_per_response')}>
        {(model.maxTokens / 1000).toFixed(0)}k {t('models.output.lowercase')}
      </Text>
    {/if}
    {#if !model.contextWindow && !model.maxTokens}
      <Text size="sm" tone="disabled">—</Text>
    {/if}
  </VStack>
{/snippet}

{#snippet modsBlock({ model }: { model: ModelsTableModel }, size: 'small' | 'large' = 'large', direction: 'vertical' | 'horizontal' = 'vertical')}
  <ModalitiesFlow
    modalities={{ input: model.inputModalities, output: model.outputModalities }}
    chipsDirection={direction}
    size={size}
  />
  {#if !hasModalities(model)}
    <Text size="sm" tone="disabled">—</Text>
  {/if}
{/snippet}

{#snippet capsBlock({ model }: { model: ModelsTableModel }, size: 'small' | 'large' = 'large')}
  <HStack gap={2} wrap class="cap-chips">
    {#each model.capabilities ?? [] as cap}
      {@const meta = CAPABILITY_META[cap]}
      <Chip
        icon={meta?.icon ?? 'help_outline'}
        text=""
        color={meta?.color ?? 'chip-neutral'}
        title={meta ? t(meta.hint) : cap}
        size={size}
      />
    {/each}
    {#if model.custom}
      <Chip icon="tune" text="" color="chip-teal" title={t('models.custom.lowercase')} size={size} />
    {/if}
    {#if !hasCapabilities(model)}
      <Text size="sm" tone="disabled">—</Text>
    {/if}
  </HStack>
{/snippet}

<div class="mt-wrap" bind:this={wrapEl}>
  <Table
    {columns}
    rows={sortedModels}
    rowKey={(model) => (model as ModelsTableModel).fullId ?? `${(model as ModelsTableModel).kind}:${(model as ModelsTableModel).id}`}
    {loading}
    {skeletonRows}
    {draggable}
    onReorder={onReorder}
    {sortKey}
    {sortDir}
    onsort={handleSort}
    rowClass={(model) => ((model as ModelsTableModel).disabled ? 'row-off' : '')}
    empty={empty}
  >
    {#snippet cell({ column, row })}
      {@const model = row as ModelsTableModel}
      {#if column.key === 'model'}
        <VStack gap={1} align="start" class="model-identity">
          {#if model.kind === 'virtual'}
            <HStack gap={2} align="center" class="identity-line">
              <Text mono size="sm" class="id-text">
                virtual/{model.id}
              </Text>
              <CopyButton size="small" text={fullId(model)} title={t('models.actions.copy_id')} ariaLabel={t('models.actions.copy_id')} />
            </HStack>
            <Text size="sm" tone="soft" class="secondary-text">{model.description || '—'}</Text>
          {:else}
            <HStack gap={2} class="identity-line">
              <Text size="base" weight="medium" class="primary-text">{model.name || model.id}</Text>
            </HStack>
            <HStack gap={2} align="center" class="identity-line">
              <Text mono size="sm" class="id-text">{fullId(model)}</Text>
              <CopyButton size="small" text={fullId(model)} title={t('models.actions.copy_id')} ariaLabel={t('models.actions.copy_id')} />
            </HStack>
          {/if}
          {#if showProviderNameLink(model)}
            <a class="provider-link" href={`#/providers/${model.providerId}`}>
              <Text size="sm">{model.providerName}</Text>
              <Icon name="chevron_right" />
            </a>
          {:else if showVirtualLink(model)}
            <a class="provider-link" href="#/virtual">
              <Text size="sm">{t('virtual.models_plural')}</Text>
              <Icon name="chevron_right" />
            </a>
          {/if}
        </VStack>
      {:else if column.key === 'context'}
        {@render ctxBlock({ model })}
      {:else if column.key === 'modalities'}
        {@render modsBlock({ model })}
      {:else if column.key === 'capabilities'}
        {@render capsBlock({ model })}
      {:else if column.key === 'ctxmods'}
        <VStack gap={2} align="start">
          {@render ctxBlock({ model })}
          {@render modsBlock({ model }, 'large', 'horizontal')}
        </VStack>
      {:else if column.key === 'all'}
        <VStack gap={4} align="start">
          {@render ctxBlock({ model })}
          {@render modsBlock({ model }, 'large', 'horizontal')}
          {@render capsBlock({ model }, 'small')}
        </VStack>
      {:else if column.key === 'actions'}
        <HStack justify="end" gap={2} class="actions-cell">
          {#if actions}{@render actions({ model })}{/if}
        </HStack>
      {/if}
    {/snippet}
  </Table>
</div>

<style>
  /* Measurement host for the merge stages: full width, no visuals. */
  .mt-wrap {
    width: 100%;
    min-width: 0;
  }
</style>
