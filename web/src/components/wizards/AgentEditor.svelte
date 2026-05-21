<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { api } from '../../lib/api'
  import Dropdown from '../Dropdown.svelte'
  import type { Agent, AgentModel, AvailableModel, DecisionModelConfig } from '../../lib/types'

  export let agent: Agent | undefined
  export let onComplete: (() => void | Promise<void>) | undefined
  export let onCancel: (() => void | Promise<void>) | undefined

  let name = agent?.name || ''
  let description = agent?.description || ''
  let models: AgentModel[] = agent?.models || []
  let instructions = agent?.instructions || { content: '', injection: 'beginning' as const }
  let useDecisionModel = !!agent?.decision_model
  let decisionModel: DecisionModelConfig = agent?.decision_model || {
    model_id: '',
    system_prompt: 'You are a routing assistant. Choose the best model for the user\'s request based on complexity, cost, and requirements.'
  }

  let availableModels: AvailableModel[] = []
  let modelsLoadState: 'loading' | 'loaded' | 'empty' | 'error' = 'loading'
  let loading = false
  let error = ''
  let saveTimeout: number | undefined
  let draftSaveInterval: number | undefined

  const SAVE_TIMEOUT = 30000
  const draftKey = `agent-draft-${agent?.id || 'new'}`

  onMount(async () => {
    if (!agent) {
      restoreDraft()
    }

    await loadAvailableModels()
    draftSaveInterval = window.setInterval(saveDraft, 5000)
  })

  onDestroy(() => {
    if (draftSaveInterval) {
      clearInterval(draftSaveInterval)
    }
    if (saveTimeout) {
      clearTimeout(saveTimeout)
    }
  })

  function restoreDraft() {
    const draft = localStorage.getItem(draftKey)
    if (!draft) {
      return
    }

    try {
      const restored = JSON.parse(draft)
      name = restored.name || ''
      description = restored.description || ''
      models = Array.isArray(restored.models) ? restored.models.filter((m: unknown) => !!m && typeof m === 'object') : []
      instructions = restored.instructions || { content: '', injection: 'beginning' }
      useDecisionModel = restored.useDecisionModel || false
      decisionModel = restored.decisionModel || decisionModel
    } catch (restoreError) {
      console.error('Failed to restore draft:', restoreError)
    }
  }

  async function loadAvailableModels() {
    try {
      const response = await api.models.available()
      availableModels = Array.isArray(response)
        ? response.filter((m: unknown) => !!m && typeof m === 'object') as AvailableModel[]
        : []
      modelsLoadState = availableModels.length === 0 ? 'empty' : 'loaded'
    } catch (e: any) {
      if (e.status === 401 || e.message?.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = (e as Error).message
      modelsLoadState = 'error'
      availableModels = []
    }
  }

  function saveDraft() {
    if (!name && !description && models.length === 0) {
      return
    }

    localStorage.setItem(draftKey, JSON.stringify({
      name,
      description,
      models,
      instructions,
      useDecisionModel,
      decisionModel,
    }))
  }

  async function save() {
    if (!canSaveAsDraft || loading) {
      return
    }

    loading = true
    error = ''

    saveTimeout = window.setTimeout(() => {
      error = 'Save operation timed out. Please check your connection and try again.'
      loading = false
    }, SAVE_TIMEOUT)

    try {
      const payload = {
        name: name.trim(),
        description: description.trim(),
        models: models.map((model, index) => ({ ...model, priority: index })),
        instructions,
        decision_model: useDecisionModel ? decisionModel : null,
        version: agent?.version || 0,
        is_draft: models.length === 0,
      }

      if (agent) {
        await api.agents.update(agent.id, payload)
      } else {
        await api.agents.create(payload)
      }

      localStorage.removeItem(draftKey)
      if (saveTimeout) {
        clearTimeout(saveTimeout)
      }

      await onComplete?.()
    } catch (e: any) {
      if (saveTimeout) {
        clearTimeout(saveTimeout)
      }

      if (e.message?.includes('modified by another process')) {
        error = 'This agent was modified elsewhere. Please refresh and try again.'
      } else if (e.message?.includes('already exists')) {
        error = 'An agent with this name already exists. Please choose a different name.'
      } else {
        error = (e as Error).message
      }

      loading = false
    }
  }

  async function cancel() {
    await onCancel?.()
  }

  function goToProviders() {
    window.location.hash = '#/providers'
  }

  function addModel() {
    const defaultModelId = availableModels.length > 0 ? availableModels[0].full_model_id : ''
    models = [...models, {
      model_id: defaultModelId,
      priority: models.length,
      description: '',
      instructions: '',
    }]
  }

  function removeModel(index: number) {
    models = models.filter((_, i) => i !== index)
  }

  function moveUp(index: number) {
    if (index === 0) {
      return
    }
    const nextModels = [...models]
    ;[nextModels[index - 1], nextModels[index]] = [nextModels[index], nextModels[index - 1]]
    models = nextModels
  }

  function moveDown(index: number) {
    if (index === models.length - 1) {
      return
    }
    const nextModels = [...models]
    ;[nextModels[index], nextModels[index + 1]] = [nextModels[index + 1], nextModels[index]]
    models = nextModels
  }

  function isModelAvailable(modelId: string): boolean {
    return availableModels.some((model) => model.full_model_id === modelId)
  }

  function modelOptions() {
    return availableModels.map((model) => ({
      value: model.full_model_id,
      label: `${model.provider_name} · ${model.display_name} · ${model.full_model_id}`,
    }))
  }

  function selectedModelLabel(modelId: string): string {
    const model = availableModels.find((entry) => entry.full_model_id === modelId)
    if (!model) {
      return `${modelId} (unavailable)`
    }
    return `${model.provider_name} · ${model.display_name} · ${model.full_model_id}`
  }

  $: canSaveAsDraft = name.trim() !== ''
  $: isComplete = name.trim() !== '' &&
    models.length > 0 &&
    (!useDecisionModel || (!!decisionModel.model_id && decisionModel.system_prompt.trim() !== ''))
  $: primaryActionLabel = agent ? 'Save' : (isComplete ? 'Create' : 'Save as Draft')
</script>

<div class="agent-editor">
  <div class="editor-actions">
    <button class="btn btn-secondary" on:click={cancel} disabled={loading}>
      Cancel
    </button>
    <button class="btn btn-primary btn-large" on:click={save} disabled={!canSaveAsDraft || loading}>
      {loading ? 'Saving…' : primaryActionLabel}
    </button>
  </div>

  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  {#if modelsLoadState === 'empty'}
    <div class="warning-banner">
      <span class="icon">warning</span>
      <div>
        <strong>No models available</strong>
        <p>Configure at least one provider with credentials to create agents.</p>
        <button class="btn btn-primary" on:click={goToProviders}>
          <span class="icon">cloud</span>
          Go to Providers
        </button>
      </div>
    </div>
  {/if}

  <section class="form-section">
    <h3>Basic Information</h3>
    <div class="form-group">
      <label for="name">Name <span class="required">*</span></label>
      <input
        id="name"
        type="text"
        class="input"
        bind:value={name}
        placeholder="e.g., Research Assistant"
      />
    </div>
    <div class="form-group">
      <label for="description">Description</label>
      <textarea
        id="description"
        class="input"
        bind:value={description}
        rows="2"
        placeholder="Optional description of what this agent does"
      />
    </div>
  </section>

  <section class="form-section">
    <h3>Models {#if models.length === 0}<span class="draft-badge">Draft</span>{/if}</h3>
    <p class="help-text">Models are tried in order (top = highest priority). Add descriptions to help the decision model choose.</p>

    {#if modelsLoadState === 'loading'}
      <div class="loading">Loading available models...</div>
    {:else if modelsLoadState === 'error'}
      <div class="error-box">
        <p>Failed to load models. Please try again.</p>
        <button class="btn btn-secondary" on:click={loadAvailableModels}>Reload</button>
      </div>
    {:else if models.length === 0}
      <div class="empty-models">
        <p>No models added yet</p>
        {#if modelsLoadState === 'empty'}
          <p class="warning-text">No models are currently available. Configure providers first.</p>
        {:else}
          <button class="btn btn-secondary" on:click={addModel}>
            <span class="icon">add</span>
            Add Model
          </button>
        {/if}
      </div>
    {:else if modelsLoadState === 'loaded'}
      {#each models as model, i}
        <div class="model-item">
          <div class="model-header">
            <span class="priority-badge">{i + 1}</span>
            <Dropdown
              bind:value={model.model_id}
              options={[
                ...modelOptions(),
                ...(!isModelAvailable(model.model_id) && model.model_id
                  ? [{ value: model.model_id, label: selectedModelLabel(model.model_id) }]
                  : []),
              ]}
              searchable={true}
              placeholder="Select a model"
            />
            <div class="model-actions">
              <button class="btn-icon" on:click={() => moveUp(i)} disabled={i === 0} title="Move up">
                <span class="icon">arrow_upward</span>
              </button>
              <button class="btn-icon" on:click={() => moveDown(i)} disabled={i === models.length - 1} title="Move down">
                <span class="icon">arrow_downward</span>
              </button>
              <button class="btn-icon btn-danger" on:click={() => removeModel(i)} title="Remove">
                <span class="icon">delete</span>
              </button>
            </div>
          </div>

          {#if !isModelAvailable(model.model_id) && model.model_id}
            <div class="warning-text">This model is no longer available.</div>
          {/if}

          <div class="form-group">
            <label for={`agent-model-desc-${i}`}>Description (for decision model)</label>
            <input
              id={`agent-model-desc-${i}`}
              type="text"
              class="input input-small"
              bind:value={model.description}
              placeholder="e.g., Best for complex reasoning and analysis"
            />
          </div>

          <div class="form-group">
            <label for={`agent-model-instr-${i}`}>Model-specific instructions (optional)</label>
            <textarea
              id={`agent-model-instr-${i}`}
              class="input input-small"
              bind:value={model.instructions}
              rows="2"
              placeholder="Additional instructions for this specific model"
            />
          </div>
        </div>
      {/each}

      <button class="btn btn-secondary" on:click={addModel}>
        <span class="icon">add</span>
        Add Model
      </button>
    {/if}
  </section>

  <section class="form-section">
    <h3>General Instructions</h3>
    <div class="form-group">
      <label for="instructions-content">Instructions</label>
      <textarea
        id="instructions-content"
        class="input"
        bind:value={instructions.content}
        rows="4"
        placeholder="System instructions that apply to all models"
      />
    </div>
    <div class="radio-group">
      <label class="radio-label">
        <input type="radio" bind:group={instructions.injection} value="beginning" />
        Inject at beginning
      </label>
      <label class="radio-label">
        <input type="radio" bind:group={instructions.injection} value="end" />
        Inject at end
      </label>
    </div>
  </section>

  <section class="form-section">
    <h3>Decision Model (Optional)</h3>
    <p class="help-text">Use a cheap model to intelligently route requests based on context.</p>

    <label class="checkbox-label">
      <input type="checkbox" bind:checked={useDecisionModel} />
      Enable decision-based routing
    </label>

    {#if useDecisionModel}
      {#if modelsLoadState === 'loading'}
        <div class="loading">Loading models...</div>
      {:else if modelsLoadState === 'empty'}
        <div class="empty-models">
          <p class="warning-text">No models available for decision routing.</p>
          <button class="btn btn-primary" on:click={goToProviders}>
            <span class="icon">cloud</span>
            Go to Providers
          </button>
        </div>
      {:else if modelsLoadState === 'loaded'}
        <div class="form-group">
          <label for="decision-model">Decision Model <span class="required">*</span></label>
          <Dropdown
            bind:value={decisionModel.model_id}
            options={[
              { value: '', label: 'Select a model...' },
              ...modelOptions(),
            ]}
            searchable={true}
            placeholder="Select a model..."
          />
        </div>
      {/if}

      <div class="form-group">
        <label for="decision-prompt">System Prompt <span class="required">*</span></label>
        <textarea
          id="decision-prompt"
          class="input"
          bind:value={decisionModel.system_prompt}
          rows="4"
          placeholder="You are a routing assistant. Choose the best model for the user's request based on complexity, cost, and requirements."
        />
      </div>
    {/if}
  </section>
</div>

<style>
  .agent-editor {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }

  .editor-actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    position: sticky;
    top: 0;
    z-index: 2;
    padding: 16px 0;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-outline-soft);
  }

  .form-section {
    padding-bottom: 32px;
    border-bottom: 1px solid var(--color-outline-soft);
  }

  .form-section:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }

  .form-section h3 {
    font-size: 16px;
    font-weight: 500;
    margin-bottom: 12px;
    color: var(--color-text);
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .draft-badge {
    font-size: 11px;
    font-weight: 500;
    padding: 2px 8px;
    background: rgba(234, 179, 8, 0.1);
    color: #ca8a04;
    border-radius: 4px;
  }

  .help-text {
    font-size: 12px;
    color: var(--color-text-soft);
    margin-bottom: 16px;
  }

  .form-group {
    margin-bottom: 16px;
  }

  .required {
    color: #dc2626;
  }

  .input {
    width: 100%;
    padding: 8px 12px;
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
    background: var(--color-surface);
    font-size: 14px;
    font-family: inherit;
    color: var(--color-text);
  }

  .input:focus {
    outline: none;
    border-color: var(--color-text-soft);
  }

  .input-small {
    font-size: 12px;
    padding: 6px 10px;
  }

  textarea.input {
    resize: vertical;
    font-family: inherit;
  }

  .radio-group {
    display: flex;
    gap: 16px;
    margin-top: 8px;
  }

  .radio-label,
  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    color: var(--color-text);
    cursor: pointer;
  }

  .checkbox-label {
    margin-bottom: 16px;
  }

  .empty-models {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    text-align: center;
    padding: 32px;
    background: var(--color-surface-container-high);
    border-radius: 8px;
    margin-bottom: 16px;
  }

  .loading {
    text-align: center;
    padding: 32px;
    color: var(--color-text-soft);
  }

  .warning-banner {
    display: flex;
    gap: 12px;
    padding: 16px;
    background: rgba(234, 179, 8, 0.1);
    border: 1px solid rgba(234, 179, 8, 0.3);
    border-radius: 8px;
  }

  .warning-banner .icon {
    font-size: 24px;
    color: #ca8a04;
    flex-shrink: 0;
  }

  .warning-banner strong {
    display: block;
    margin-bottom: 4px;
    color: var(--color-text);
  }

  .warning-banner p {
    font-size: 14px;
    color: var(--color-text-soft);
    margin-bottom: 12px;
  }

  .warning-text {
    font-size: 12px;
    color: #ca8a04;
  }

  .error-box {
    padding: 16px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 8px;
    margin-bottom: 16px;
    text-align: center;
  }

  .error-box p {
    margin-bottom: 12px;
    color: #dc2626;
  }

  .model-item {
    background: var(--color-surface-container-high);
    border: 1px solid var(--color-outline-soft);
    border-radius: 8px;
    padding: 16px;
    margin-bottom: 12px;
  }

  .model-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 12px;
  }

  .priority-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    background: var(--color-text);
    color: #ffffff;
    border-radius: 50%;
    font-size: 12px;
    font-weight: 600;
    flex-shrink: 0;
  }

  .model-header :global(.dropdown) {
    flex: 1;
    min-width: 0;
  }

  .model-actions {
    display: flex;
    gap: 4px;
  }

  .btn-icon {
    width: 32px;
    height: 32px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
    cursor: pointer;
  }

  .btn-icon:hover:not(:disabled) {
    background: var(--color-button-container-high);
  }

  .btn-icon:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .btn-icon.btn-danger {
    border-color: #dc2626;
    color: #dc2626;
  }

  .btn-icon.btn-danger:hover:not(:disabled) {
    background: rgba(239, 68, 68, 0.1);
  }
</style>
