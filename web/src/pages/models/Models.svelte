<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { AvailableModel, VirtualModel } from '$lib/types'
  import { t } from '$lib/i18n.svelte'
  import { HStack, SearchField, ModelsTable, Text, VStack } from '$ui'
  import type { ModelsTableModel } from '$ui'

  const MIN_FUZZY_SCORE = 0.72

  let allModels = $state<AvailableModel[]>([])
  let virtualModels = $state<VirtualModel[]>([])
  let loading = $state(true)
  let error = $state('')
  let query = $state('')

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
      const members = (vm.models ?? []).map((e) => candidateById.get(e.model_id))
      const union = (pick: (m: AvailableModel | undefined) => string[] | undefined): string[] => {
        const out: string[] = []
        for (const m of members) for (const x of pick(m) ?? []) if (!out.includes(x)) out.push(x)
        return out
      }
      let contextWindow = 0
      let maxTokens = 0
      for (const m of members) {
        if (m?.context_window && (!contextWindow || m.context_window < contextWindow)) contextWindow = m.context_window
        if (m?.max_tokens && (!maxTokens || m.max_tokens < maxTokens)) maxTokens = m.max_tokens
      }
      rows.push({
        kind: 'virtual' as const,
        id: vm.id,
        fullId: `virtual/${vm.id}`,
        description: vm.description || '—',
        contextWindow: contextWindow || undefined,
        maxTokens: maxTokens || undefined,
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


  function normalize(value: string): string {
    return value.trim().toLowerCase()
  }

  function levenshtein(a: string, b: string): number {
    const rows = a.length + 1
    const cols = b.length + 1
    const dp = Array.from({ length: rows }, () => new Array<number>(cols).fill(0))

    for (let i = 0; i < rows; i += 1) {
      dp[i][0] = i
    }
    for (let j = 0; j < cols; j += 1) {
      dp[0][j] = j
    }

    for (let i = 1; i < rows; i += 1) {
      for (let j = 1; j < cols; j += 1) {
        const cost = a[i - 1] === b[j - 1] ? 0 : 1
        dp[i][j] = Math.min(dp[i - 1][j] + 1, dp[i][j - 1] + 1, dp[i - 1][j - 1] + cost)
      }
    }

    return dp[rows - 1][cols - 1]
  }

  function similarity(queryValue: string, candidate: string): number {
    if (!queryValue || !candidate) {
      return 0
    }
    const maxLength = Math.max(queryValue.length, candidate.length)
    if (maxLength === 0) {
      return 1
    }
    return 1 - levenshtein(queryValue, candidate) / maxLength
  }

  type RankedModel = AvailableModel & { category: number; score: number }

  function rankModels(models: AvailableModel[], rawQuery: string): RankedModel[] {
    const normalizedQuery = normalize(rawQuery)
    if (!normalizedQuery) {
      return models.map((model) => ({ ...model, category: 3, score: 0 }))
    }

    const ranked: RankedModel[] = []
    for (const model of models) {
      const fields = [
        normalize(model.full_model_id),
        normalize(model.model_name),
        normalize(model.provider_name),
        normalize(`${model.provider_name} ${model.display_name} ${model.full_model_id}`)
      ]

      const prefixMatch = fields.some((field) => field.startsWith(normalizedQuery))
      if (prefixMatch) {
        ranked.push({ ...model, category: 0, score: 1 })
        continue
      }

      const substringMatch = fields.some((field) => field.includes(normalizedQuery))
      if (substringMatch) {
        ranked.push({ ...model, category: 1, score: 1 })
        continue
      }

      const bestScore = Math.max(...fields.map((field) => similarity(normalizedQuery, field)))
      if (bestScore >= MIN_FUZZY_SCORE) {
        ranked.push({ ...model, category: 2, score: bestScore })
      }
    }

    return ranked
  }
</script>

<VStack gap={4}>
  <HStack gap={4} align="center">
    <Text tag="h1" size="lg" weight="bold">{t('Models')}</Text>
    {#if !loading && allModels.length + virtualModels.length > 0}
      <Text tone="soft" size="base">{allModels.length + virtualModels.length}</Text>
    {/if}
  </HStack>

  {#if error}
    <Text tone="danger" size="sm">{error}</Text>
  {/if}

  <SearchField bind:value={query} placeholder={t('Search by model name, provider, or full model ID')} />

  <ModelsTable models={tableModels} readonly sortable showProviderLink loading={loading}>
    {#snippet empty()}
      <Text tone="soft" size="sm">
        {#if allModels.length + virtualModels.length === 0}
          {t('No available models yet. Configure providers with working credentials first.')}
        {:else}
          {t('No matching models found.')}
        {/if}
      </Text>
    {/snippet}
  </ModelsTable>
</VStack>
