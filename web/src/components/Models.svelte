<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import type { AvailableModel } from '../lib/types'

  const MIN_FUZZY_SCORE = 0.72

  let allModels: AvailableModel[] = []
  let loading = true
  let error = ''
  let query = ''
  let copiedModelId = ''

  onMount(load)

  async function load() {
    loading = true
    error = ''
    try {
      const response = await api.models.available()
      allModels = Array.isArray(response) ? response as AvailableModel[] : []
    } catch (e) {
      error = (e as Error).message
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
      error = (e as Error).message || 'Failed to copy model ID'
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
        dp[i][j] = Math.min(
          dp[i - 1][j] + 1,
          dp[i][j - 1] + 1,
          dp[i - 1][j - 1] + cost,
        )
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
    return 1 - (levenshtein(queryValue, candidate) / maxLength)
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
        normalize(`${model.provider_name} ${model.display_name} ${model.full_model_id}`),
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

  $: rankedModels = rankModels(allModels, query)
  $: filteredModels = [...rankedModels].sort((a, b) => {
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
</script>

<div class="page-header">
  <div>
    <h1>Models</h1>
    <p>Browse available models and quickly copy their full model IDs.</p>
  </div>
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

<div class="toolbar card">
  <div class="toolbar-inner">
    <div class="search-group">
      <label for="model-search">Search</label>
      <input
        id="model-search"
        type="text"
        bind:value={query}
        placeholder="Search by model name, provider, or full model ID"
      />
    </div>
    <div class="result-count">
      {filteredModels.length} result{filteredModels.length === 1 ? '' : 's'}
    </div>
  </div>
</div>

{#if loading}
  <div class="empty">Loading models…</div>
{:else if allModels.length === 0}
  <div class="empty">No available models yet. Configure providers with working credentials first.</div>
{:else if filteredModels.length === 0}
  <div class="empty">No matching models found.</div>
{:else}
  <div class="card">
    <div class="card-header">
      <h2>Available Models</h2>
    </div>
    <table>
      <thead>
        <tr>
          <th>Display</th>
          <th>Provider</th>
          <th>Full Model ID</th>
          <th>Context</th>
          <th>Max Tokens</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each filteredModels as model}
          <tr>
            <td>
              <div class="display-cell">
                <strong>{model.display_name}</strong>
                {#if model.display_name !== model.model_name}
                  <span class="subtle">{model.model_name}</span>
                {/if}
              </div>
            </td>
            <td>{model.provider_name}</td>
            <td><code class="mono">{model.full_model_id}</code></td>
            <td>{model.context_window || '—'}</td>
            <td>{model.max_tokens || '—'}</td>
            <td class="copy-cell">
              <button class="btn btn-secondary btn-sm" on:click={() => copyModelId(model.full_model_id)}>
                <span class="icon">{copiedModelId === model.full_model_id ? 'check' : 'content_copy'}</span>
                {copiedModelId === model.full_model_id ? 'Copied' : 'Copy ID'}
              </button>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .toolbar {
    margin-bottom: 24px;
  }

  .toolbar-inner {
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    gap: 24px;
    padding: 20px 24px;
  }

  .search-group {
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex: 1;
  }

  .search-group label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
  }

  .result-count {
    color: var(--color-text-soft);
    font-size: 13px;
    white-space: nowrap;
  }

  .display-cell {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .subtle {
    color: var(--color-text-soft);
    font-size: 12px;
  }

  .copy-cell {
    text-align: right;
    white-space: nowrap;
  }

  .empty {
    padding: 48px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }

  @media (max-width: 900px) {
    .toolbar-inner {
      flex-direction: column;
      align-items: stretch;
    }

    .result-count {
      white-space: normal;
    }
  }
</style>
