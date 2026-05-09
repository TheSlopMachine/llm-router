<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { api } from '../../lib/api'
  import Dropdown from '../Dropdown.svelte'
  import type { Agent, AgentModel, DecisionModelConfig, ModelInfo } from '../../lib/types'

  export let agent: Agent | undefined
  export let updateButtons: (buttons: any[]) => void
  export let closeModal: () => void
  export let onComplete: () => void

  let name = agent?.name || ''
  let description = agent?.description || ''
  let models: AgentModel[] = agent?.models || []
  let instructions = agent?.instructions || { content: '', injection: 'beginning' as const }
  let useDecisionModel = !!agent?.decision_model
  let decisionModel: DecisionModelConfig = agent?.decision_model || { 
    model_id: '', 
    system_prompt: 'You are a routing assistant. Choose the best model for the user\'s request based on complexity, cost, and requirements.' 
  }

  let availableModels: ModelInfo[] = []
  let modelsLoadState: 'loading' | 'loaded' | 'empty' | 'error' = 'loading'
  let loading = false
  let error = ''
  let saveTimeout: number | undefined

  const SAVE_TIMEOUT = 30000
  const draftKey = `agent-draft-${agent?.id || 'new'}`
  let draftSaveInterval: number | undefined

  onMount(async () => {
    // Try to restore draft
    if (!agent) {
      const draft = localStorage.getItem(draftKey)
      if (draft) {
        try {
          const restored = JSON.parse(draft)
          name = restored.name || ''
          description = restored.description || ''
          models = (restored.models || []).filter((m: any) => m && typeof m === 'object')
          instructions = restored.instructions || { content: '', injection: 'beginning' }
          useDecisionModel = restored.useDecisionModel || false
          decisionModel = restored.decisionModel || { model_id: '', system_prompt: '' }
        } catch (e) {
          console.error('Failed to restore draft:', e)
        }
      }
    }

    // Load available models
    try {
      const response = await api.agents.availableModels()
      // Filter out any null/undefined elements
      availableModels = Array.isArray(response) 
        ? response.filter((m: any) => m && typeof m === 'object') 
        : []
      modelsLoadState = availableModels.length === 0 ? 'empty' : 'loaded'
    } catch (e: any) {
      // Handle authentication errors
      if (e.status === 401 || e.message?.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = (e as Error).message
      modelsLoadState = 'error'
      availableModels = []
    }
    
    updateStepButtons()

    // Auto-save draft every 5 seconds
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

  function saveDraft() {
    if (name || description || models.length > 0) {
      localStorage.setItem(draftKey, JSON.stringify({
        name, description, models, instructions, useDecisionModel, decisionModel
      }))
    }
  }

  function updateStepButtons() {
    const canSaveAsDraft = name.trim() !== ''
    const isComplete = name.trim() !== '' && models.length > 0 && 
      (!useDecisionModel || (decisionModel.model_id && decisionModel.system_prompt.trim()))

    updateButtons([
      { label: 'Cancel', variant: 'secondary', onClick: closeModal },
      { 
        label: agent ? 'Save' : (isComplete ? 'Create' : 'Save as Draft'), 
        variant: 'primary', 
        onClick: save, 
        disabled: !canSaveAsDraft,
        loading 
      }
    ])
  }

  $: {
    name, description, models, instructions, useDecisionModel, decisionModel
    updateStepButtons()
  }

  async function save() {
    loading = true
    error = ''

    // Set timeout
    saveTimeout = window.setTimeout(() => {
      error = 'Save operation timed out. Please check your connection and try again.'
      loading = false
      updateStepButtons()
    }, SAVE_TIMEOUT)

    try {
      const payload = {
        name: name.trim(),
        description: description.trim(),
        models: models.map((m, i) => ({ ...m, priority: i })),
        instructions,
        decision_model: useDecisionModel ? decisionModel : null,
        version: agent?.version || 0,
        is_draft: models.length === 0
      }

       if (agent) {
         await api.agents.update(agent.id, payload)
       } else {
         await api.agents.create(payload)
       }
       
       if (onComplete) await onComplete()

       // Clear draft and timeout
      localStorage.removeItem(draftKey)
      if (saveTimeout) clearTimeout(saveTimeout)
      
      closeModal()
    } catch (e: any) {
      if (saveTimeout) clearTimeout(saveTimeout)
      
      // Handle specific errors
      if (e.message?.includes('modified by another process')) {
        error = 'This agent was modified elsewhere. Please refresh and try again.'
      } else if (e.message?.includes('already exists')) {
        error = 'An agent with this name already exists. Please choose a different name.'
      } else {
        error = (e as Error).message
      }
      
      loading = false
      updateStepButtons()
    }
  }

  function addModel() {
    const defaultModelId = availableModels.length > 0 ? availableModels[0].name : ''
    models = [...models, { 
      model_id: defaultModelId, 
      priority: models.length, 
      description: '', 
      instructions: '' 
    }]
  }

  function removeModel(index: number) {
    models = models.filter((_, i) => i !== index)
  }

  function moveUp(index: number) {
    if (index === 0) return
    const newModels = [...models]
    ;[newModels[index - 1], newModels[index]] = [newModels[index], newModels[index - 1]]
    models = newModels
  }

  function moveDown(index: number) {
    if (index === models.length - 1) return
    const newModels = [...models]
    ;[newModels[index], newModels[index + 1]] = [newModels[index + 1], newModels[index]]
    models = newModels
  }

  function isModelAvailable(modelId: string): boolean {
    return availableModels.some(m => m.name === modelId)
  }
</script>

<div class="agent-editor">
  {#if error}
    <p class="error-text">Error loading models: {error}</p>
  {/if}

  {#if modelsLoadState === 'empty'}
    <div class="warning-banner">
      <span class="icon">warning</span>
      <div>
        <strong>No models available</strong>
        <p>Configure at least one provider with credentials to create agents.</p>
        <button class="btn btn-primary" on:click={() => { closeModal(); window.location.hash = '#/providers' }}>
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
        <button class="btn btn-secondary" on:click={() => window.location.reload()}>Reload</button>
      </div>
    {:else if models.length === 0}
      <div class="empty-models">
        <p>No models added yet</p>
        {#if modelsLoadState === 'empty'}
          <p class="warning-text">⚠️ No models available. Configure providers first.</p>
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
            <span class="priority-badge">{i}</span>
            <Dropdown
              bind:value={model.model_id}
              options={[
                ...availableModels.map(m => ({
                  value: m.name,
                  label: m.display_name || m.name
                })),
                // Show current value even if not in available list
                ...(!isModelAvailable(model.model_id) && model.model_id 
                  ? [{value: model.model_id, label: `${model.model_id} (unavailable)`}]
                  : [])
              ]}
              searchable={true}
              placeholder="Select a model"
            />
            <div class="model-actions">
              <button 
                class="btn-icon" 
                on:click={() => moveUp(i)} 
                disabled={i === 0}
                title="Move up"
              >
                <span class="icon">arrow_upward</span>
              </button>
              <button 
                class="btn-icon" 
                on:click={() => moveDown(i)} 
                disabled={i === models.length - 1}
                title="Move down"
              >
                <span class="icon">arrow_downward</span>
              </button>
              <button 
                class="btn-icon btn-danger" 
                on:click={() => removeModel(i)}
                title="Remove"
              >
                <span class="icon">delete</span>
              </button>
            </div>
          </div>
          
          {#if !isModelAvailable(model.model_id) && model.model_id}
            <div class="warning-text">⚠️ This model is no longer available</div>
          {/if}
          
           <div class="form-group">
             <label for="agent-model-desc">Description (for decision model)</label>
             <input 
               id="agent-model-desc"
               type="text"
               class="input input-small" 
               bind:value={model.description} 
               placeholder="e.g., Best for complex reasoning and analysis"
             />
           </div>
           
           <div class="form-group">
             <label for="agent-model-instr">Model-specific instructions (optional)</label>
             <textarea 
               id="agent-model-instr"
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
        <input 
          type="radio" 
          bind:group={instructions.injection} 
          value="beginning"
        />
        Inject at beginning
      </label>
      <label class="radio-label">
        <input 
          type="radio" 
          bind:group={instructions.injection} 
          value="end"
        />
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
<div class="bg-surface">
  <p class="warning-text">⚠️ No models available for decision routing.</p>
  <p class="help-text">Add providers and credentials first.</p>
</div>
      {:else if modelsLoadState === 'loaded'}
        <div class="form-group">
          <label for="decision-model">Decision Model <span class="required">*</span></label>
          <Dropdown
            bind:value={decisionModel.model_id}
            options={[
              {value: '', label: 'Select a model...'},
              ...availableModels.map(m => ({
                value: m.name,
                label: m.display_name || m.name
              }))
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
    max-height: 70vh;
    overflow-y: auto;
    padding: 4px;
  }

  .form-section {
    margin-bottom: 32px;
    padding-bottom: 32px;
    border-bottom: 1px solid var(--color-outline-soft);
  }

  .form-section:last-child {
    border-bottom: none;
    margin-bottom: 0;
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

  .form-group label {
    display: block;
    font-size: 12px;
    font-weight: 500;
    margin-bottom: 6px;
    color: var(--color-text);
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

  .radio-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 14px;
    color: var(--color-text);
    cursor: pointer;
  }

  .checkbox-label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    color: var(--color-text);
    cursor: pointer;
    margin-bottom: 16px;
  }

  .empty-models {
    text-align: center;
    padding: 32px;
    background: var(--color-surface-container-high);
    border-radius: 8px;
    margin-bottom: 16px;
  }

  .empty-models p {
    color: var(--color-text-soft);
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
    margin-bottom: 24px;
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

  .warning-box {
    padding: 16px;
    background: rgba(234, 179, 8, 0.1);
    border: 1px solid rgba(234, 179, 8, 0.3);
    border-radius: 8px;
    margin-bottom: 16px;
  }

  .warning-box p {
    margin: 0 0 8px 0;
    color: var(--color-text);
  }

  .warning-text {
    font-size: 12px;
    color: #ca8a04;
    margin-bottom: 8px;
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
    transition: background 0.15s;
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

  .error-msg {
    background: rgba(239, 68, 68, 0.1);
    color: #dc2626;
    padding: 12px;
    border-radius: 8px;
    margin-bottom: 16px;
    font-size: 14px;
  }
</style>
