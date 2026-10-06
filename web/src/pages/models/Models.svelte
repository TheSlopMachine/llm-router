<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { AvailableModel, VirtualModel } from '$lib/types'
  import { t } from '$lib/i18n.svelte'
  import { HStack, SearchField, ModelsTable, Text, VStack, Banner } from '$ui'
  import type { ModelsTableModel } from '$ui'
  import EmptyState from '../../components/EmptyState.svelte'
  import { rankModels, normalize } from '$lib/model-search'
  import type { Ranked } from '$lib/model-search'
  import { unionMembers as unionIds, minPositive } from '$lib/model-aggregates'

  let allModels = $state<AvailableModel[]>([])
  let virtualModels = $state<VirtualModel[]>([])
  let loading = $state(true)
  let error = $state('')
  let query = $state('')

  let hasModels = $derived(!loading && allModels.length + virtualModels.length > 0)
  let isEmptyCatalog = $derived(allModels.length + virtualModels.length === 0)

  let rankedModels = $derived(rankModels(allModels, query))
  let filteredModels = $derived(
    [...rankedModels].sort((a, b) => {
      if (a.category !== b.category) {
        return a.category - b.category
      }
      if (a.category === 2 && a.score !== b.score) {
        return b.score - a.score
      }
      if (a.provider_name !== b.provider_name) {
        return a.provider_name.localeCompare(b.provider_name)
      }
      if (a.display_name !== b.display_name) {
        return a.display_name.localeCompare(b.display_name)
      }
      return a.full_model_id.localeCompare(b.full_model_id)
    })
  )

  const candidateById = $derived(new Map(allModels.map((m) => [m.full_model_id, m])))

  let tableModels = $derived.by((): ModelsTableModel[] => {
    const rows: ModelsTableModel[] = filteredModels.map((model) => ({
      kind: 'model' as const,
      id: model.model_name,
      fullId: model.full_model_id,
      name: model.display_name,
      providerId: model.provider_id,
      providerName: model.provider_name,
      contextWindow: model.context_window,
      maxTokens: model.max_tokens,
      inputModalities: model.input_modalities,
      outputModalities: model.output_modalities,
      capabilities: model.capabilities,
    }))
    const q = normalize(query)
    for (const vm of virtualModels) {
      if (q && ![vm.id, vm.name, vm.description ?? '', `virtual/${vm.id}`].some((v) => normalize(v).includes(q))) continue
      const memberIds = (vm.models ?? []).map((e) => e.model_id)
      const members = memberIds.map((id) => candidateById.get(id))
      const union = (pick: (m: AvailableModel | undefined) => string[] | undefined): string[] =>
        unionIds(memberIds, (id) => pick(candidateById.get(id)))
      const contextWindow = minPositive(members.map((m) => m?.context_window)) || undefined
      const maxTokens = minPositive(members.map((m) => m?.max_tokens)) || undefined
      rows.push({
        kind: 'virtual' as const,
        id: vm.id,
        fullId: `virtual/${vm.id}`,
        description: vm.description || '—',
        contextWindow,
        maxTokens,
        inputModalities: union((m) => m?.input_modalities),
        outputModalities: union((m) => m?.output_modalities),
        capabilities: union((m) => m?.capabilities),
        disabled: vm.disabled,
      })
    }
    return rows
  })

  onMount(load)

  async function load() {
    loading = true
    error = ''
    try {
      const [modelsRes, vmsRes] = await Promise.all([
        api.models.available(),
        api.virtualModels.list().catch(() => []),
      ])
      allModels = Array.isArray(modelsRes) ? (modelsRes as AvailableModel[]) : []
      const vms = vmsRes as unknown
      virtualModels = Array.isArray(vms) ? (vms as VirtualModel[]) : []
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }


  type RankedModel = Ranked<AvailableModel>
</script>

<VStack gap={4}>
  <HStack gap={4} align="center">
    <Text tag="h1" size="lg" weight="bold">{t('models.list.title')}</Text>
    {#if hasModels}
      <Text tone="soft" size="base">{allModels.length + virtualModels.length}</Text>
    {/if}
  </HStack>

  {#if error}
    <Banner variant="error" text={error} />
  {/if}

  <SearchField bind:value={query} placeholder={t('models.search.full_id')} />

  <ModelsTable models={tableModels} readonly sortable showProviderLink loading={loading}>
    {#snippet empty()}
      {#if isEmptyCatalog}
        <EmptyState title={t('models.list.empty_configure_first')} icon="cloud" />
      {:else}
        <EmptyState title={t('models.list.no_match')} icon="search" />
      {/if}
    {/snippet}
  </ModelsTable>
</VStack>
