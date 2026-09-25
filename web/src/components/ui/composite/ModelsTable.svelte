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
  import VStack from '../layout/VStack.svelte'
  import HStack from '../layout/HStack.svelte'
  import Text from '../controls/Text.svelte'
  import { CAPABILITY_META } from '../../../lib/capabilities'
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

  const columns = $derived<TableColumn[]>([
    { key: 'model', title: t('Model'), width: readonly ? '3.6fr' : '3.2fr', sortable },
    { key: 'context', title: t('Context'), width: '0.9fr', sortable },
    { key: 'modalities', title: t('Modalities'), width: '1.2fr', sortable, align: 'center' },
    { key: 'capabilities', title: t('Capabilities'), width: readonly ? '1.1fr' : '1.2fr', sortable, align: 'left' },
    ...(!readonly ? [{ key: 'actions', title: t('Actions'), width: '0.9fr', align: 'right' as const }] : []),
  ])

  const sortedModels = $derived.by(() => {
    const key = sortKey
    const dir = sortDir
    if (!sortable || !key || !dir) return models

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
    return model.fullId ?? (model.kind === 'virtual' ? `virtual/${model.id}` : model.providerId ? `${model.providerId}/${model.id}` : model.id)
  }

  function openProvider(providerId: string): void {
    window.location.hash = `#/providers/${providerId}`
  }
</script>

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
              <CopyButton size="small" text={fullId(model)} title={t('Copy model id')} ariaLabel={t('Copy model id')} />
            </HStack>
            <Text size="sm" tone="soft" class="secondary-text">{model.description || '—'}</Text>
          {:else}
            <HStack gap={2} class="identity-line">
              <Text size="base" weight="medium" class="primary-text">{model.name || model.id}</Text>
            </HStack>
            <HStack gap={2} align="center" class="identity-line">
              <Text mono size="sm" class="id-text">{fullId(model)}</Text>
              <CopyButton size="small" text={fullId(model)} title={t('Copy model id')} ariaLabel={t('Copy model id')} />
            </HStack>
          {/if}
          {#if showProviderLink && model.providerId && model.providerName}
            <a class="provider-link" href={`#/providers/${model.providerId}`}>
              <Text size="sm">{model.providerName}</Text>
              <span class="icon" aria-hidden="true">chevron_right</span>
            </a>
          {:else if showProviderLink && model.kind === 'virtual'}
            <a class="provider-link" href="#/virtual">
              <Text size="sm">{t('Virtual models')}</Text>
              <span class="icon" aria-hidden="true">chevron_right</span>
            </a>
          {/if}
        </VStack>
      {:else if column.key === 'context'}
        <VStack gap={1} align="start" class="ctx-text">
          {#if model.contextWindow}
            <Text size="sm" tone="soft" title={t('Context window — up to') + ` ${model.contextWindow.toLocaleString()} ` + t('input tokens')}>
              {(model.contextWindow / 1000).toFixed(0)}k {t('context')}
            </Text>
          {/if}
          {#if model.maxTokens}
            <Text size="sm" tone="soft" title={t('Max output — up to') + ` ${model.maxTokens.toLocaleString()} ` + t('tokens per response')}>
              {(model.maxTokens / 1000).toFixed(0)}k {t('output')}
            </Text>
          {/if}
          {#if !model.contextWindow && !model.maxTokens}
            <Text size="sm" tone="disabled">—</Text>
          {/if}
        </VStack>
      {:else if column.key === 'modalities'}
        <ModalitiesFlow
          modalities={{ input: model.inputModalities, output: model.outputModalities }}
          chipsDirection="vertical"
          size="large"
        />
        {#if (model.inputModalities?.length ?? 0) === 0 && (model.outputModalities?.length ?? 0) === 0}
          <Text size="sm" tone="disabled">—</Text>
        {/if}
      {:else if column.key === 'capabilities'}
        <HStack gap={2} wrap class="cap-chips">
          {#each model.capabilities ?? [] as cap}
            {@const meta = CAPABILITY_META[cap]}
            <Chip
              icon={meta?.icon ?? 'help_outline'}
              text=""
              color={meta?.color ?? 'chip-neutral'}
              title={meta ? t(meta.hint) : cap}
              size="large"
            />
          {/each}
          {#if model.custom}
            <Chip icon="tune" text="" color="chip-teal" title={t('Custom model')} />
          {/if}
          {#if (model.capabilities?.length ?? 0) === 0 && !model.custom}
            <Text size="sm" tone="disabled">—</Text>
          {/if}
        </HStack>
      {:else if column.key === 'actions'}
        <HStack justify="end" gap={2} class="actions-cell">
          {#if actions}{@render actions({ model })}{/if}
        </HStack>
      {/if}
    {/snippet}
  </Table>
