<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import { squircle } from '../lib/squircle'
  import SearchField from './ui/SearchField.svelte'
  import type { AvailableModel } from '../lib/types'
  import { t } from '../lib/i18n.svelte'

  const MIN_FUZZY_SCORE = 0.72

  let allModels = $state<AvailableModel[]>([])
  let loading = $state(true)
  let error = $state('')
  let query = $state('')
  let copiedModelId = $state('')

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

  onMount(load)

  async function load() {
    loading = true
    error = ''
    try {
      const response = await api.models.available()
      allModels = Array.isArray(response) ? (response as AvailableModel[]) : []
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function copyModelId(modelId: string) {
    try {
      await navigator.clipboard.writeText(modelId)
      copiedModelId = modelId
      window.setTimeout(() => {
        if (copiedModelId === modelId) {
          copiedModelId = ''
        }
      }, 1500)
    } catch (e) {
      error = getErrorMessage(e) || 'Failed to copy model ID'
    }
  }

  // Same presentation as the provider-detail models table: "128k context".
  function fmtCtx(n: number | undefined | null, kind: 'context' | 'output'): string {
    if (!n) return '—'
    return `${(n / 1000).toFixed(0)}k ${t(kind)}`
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

<div class="page-header">
  <div class="title-row">
    <h1>{t('Models')}</h1>
    {#if !loading && allModels.length > 0}
      <span class="title-count">{allModels.length}</span>
    {/if}
  </div>
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

<SearchField bind:value={query} placeholder={t('Search by model name, provider, or full model ID')} />

{#if loading}
  <div class="empty">{t('Loading models…')}</div>
{:else if allModels.length === 0}
  <div class="empty">{t('No available models yet. Configure providers with working credentials first.')}</div>
{:else if filteredModels.length === 0}
  <div class="empty">{t('No matching models found.')}</div>
{:else}
  <div class="table" use:squircle={18}>
    <div class="table-row table-head">
      <span class="col-display">{t('Model')}</span>
      <span class="col-ctx">{t('Context')}</span>
      <span class="col-id">{t('Model ID')}</span>
    </div>
    {#each filteredModels as model (model.full_model_id)}
      <div class="table-row">
        <span class="col-display">
          <span class="display-name">{model.display_name}</span>
          <a
            class="provider-link"
            href={model.provider_type === 'virtual' ? '#/virtual' : `#/providers/${model.provider_id}`}
          >{model.provider_type === 'virtual' ? t('Virtual models') : model.provider_name}<span class="icon">chevron_right</span></a>
        </span>
        <span class="col-ctx ctx-text">
          {#if model.context_window}
            <span title={t('Context window — up to') + ` ${model.context_window.toLocaleString()} ` + t('input tokens')}>{fmtCtx(model.context_window, 'context')}</span>
          {/if}
          {#if model.max_tokens}
            <span title={t('Max output — up to') + ` ${model.max_tokens.toLocaleString()} ` + t('tokens per response')}>{fmtCtx(model.max_tokens, 'output')}</span>
          {/if}
          {#if !model.context_window && !model.max_tokens}—{/if}
        </span>
        <span class="col-id">
          <span class="mono id-text">{model.full_model_id}</span>
          <button
            class="btn-icon btn-sm copy-btn"
            class:icon-ok={copiedModelId === model.full_model_id}
            onclick={() => copyModelId(model.full_model_id)}
            aria-label={t('Copy model id')}
            title={t('Copy model id')}
            use:squircle={8}
          >
            <span class="icon">{copiedModelId === model.full_model_id ? 'check' : 'content_copy'}</span>
          </button>
        </span>
      </div>
    {/each}
  </div>
{/if}

<style>
  .title-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  /* The global page-header h1 carries margin-bottom; it would offset the
     count's centering against the title. */
  .title-row h1 {
    margin: 0;
  }

  .title-count {
    font-size: 14px;
    font-weight: 400;
    color: var(--color-text-soft);
  }

  /* Column layout only — table widget chrome comes from the global rules. */
  .table-row {
    grid-template-columns: minmax(0, 1.4fr) minmax(0, 0.6fr) minmax(0, 1.3fr);
  }

  .col-display {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .display-name {
    font-size: 14px;
    font-weight: 500;
    line-height: 18px;
    color: var(--color-text);
    /* wrap at spaces instead of clipping with an ellipsis */
    white-space: normal;
    overflow-wrap: break-word;
  }

  .provider-link {
    display: inline-flex;
    align-items: center;
    gap: 1px;
    min-width: 0;
    width: fit-content;
    font-size: 12px;
    line-height: 16px;
    color: var(--color-text-soft);
    text-decoration: none;
    overflow: hidden;
    white-space: nowrap;
  }

  .provider-link:hover {
    color: var(--color-text);
    text-decoration: none;
  }

  .provider-link .icon {
    font-size: 14px;
    color: var(--color-text-disabled);
  }

  /* Same presentation as the provider-detail models table. */
  .ctx-text {
    display: flex;
    flex-direction: column;
    gap: 1px;
    font-size: 13px;
    color: var(--color-text-soft);
    white-space: nowrap;
  }

  .col-id {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .id-text {
    font-size: 12px;
    color: var(--color-text-soft);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .copy-btn {
    flex-shrink: 0;
  }

  .icon-ok {
    color: var(--color-success-text);
  }

  @media (max-width: 720px) {
    .table-row {
      grid-template-columns: minmax(0, 1.4fr) minmax(0, 1.2fr);
      padding: 10px 12px;
    }
    .col-ctx {
      display: none;
    }
  }
</style>
