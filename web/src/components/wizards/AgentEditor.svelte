<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import Dropdown from '../Dropdown.svelte'
  import Switch from '../ui/Switch.svelte'
  import SectionCard from '../ui/SectionCard.svelte'
  import SegmentedControl from '../ui/SegmentedControl.svelte'
  import ModelCard from './agents/ModelCard.svelte'
  import type { Agent, AgentModel, AvailableModel, DecisionModelConfig } from '../../lib/types'

  let {
    agent,
    onComplete,
    onCancel
  } = $props<{
    agent?: Agent
    onComplete?: (() => void | Promise<void>) | undefined
    onCancel?: (() => void | Promise<void>) | undefined
  }>()

  let name = $state(agent?.name || '')
  let description = $state(agent?.description || '')
  let models = $state<AgentModel[]>(agent?.models ? [...agent.models] : [])
  let instructions = $state(agent?.instructions || { content: '', injection: 'beginning' as const })
  let useDecisionModel = $state(!!agent?.decision_model)
  let decisionModel = $state<DecisionModelConfig>(
    agent?.decision_model || {
      model_id: '',
      system_prompt: 'You are a routing assistant. Choose the best model for the user\'s request based on complexity, cost, and requirements.'
    }
  )

  let availableModels = $state<AvailableModel[]>([])
  let modelsLoadState = $state<'loading' | 'loaded' | 'empty' | 'error'>('loading')
  let loading = $state(false)
  let error = $state('')
  let saveTimeout: ReturnType<typeof setTimeout> | undefined = $state(undefined)
  let draftSaveInterval: ReturnType<typeof setInterval> | undefined = $state(undefined)

  const SAVE_TIMEOUT = 30000
  const draftKey = `agent-draft-${agent?.id || 'new'}`

  onMount(() => {
    if (!agent) restoreDraft()
    void loadAvailableModels()
    const interval = window.setInterval(saveDraft, 5000)
    draftSaveInterval = interval
    return () => {
      clearInterval(interval as unknown as number)
      if (saveTimeout) clearTimeout(saveTimeout as unknown as number)
    }
  })

  function restoreDraft() {
    const draft = localStorage.getItem(draftKey)
    if (!draft) return
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
      availableModels = Array.isArray(response) ? (response.filter((m: unknown) => !!m && typeof m === 'object') as AvailableModel[]) : []
      modelsLoadState = availableModels.length === 0 ? 'empty' : 'loaded'
    } catch (e: unknown) {
      const msg = getErrorMessage(e)
      if ((e as { status?: number })?.status === 401 || msg.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = msg
      modelsLoadState = 'error'
      availableModels = []
    }
  }

  function saveDraft() {
    if (!name && !description && models.length === 0) return
    localStorage.setItem(draftKey, JSON.stringify({ name, description, models, instructions, useDecisionModel, decisionModel }))
  }

  async function save() {
    if (!canSaveAsDraft || loading) return
    loading = true
    error = ''
    saveTimeout = window.setTimeout(() => {
      error = 'Save operation timed out. Please check your connection and try again.'
      loading = false
    }, SAVE_TIMEOUT) as unknown as ReturnType<typeof setTimeout>
    try {
      const payload = {
        name: name.trim(),
        description: description.trim(),
        models: models.map((model, index) => ({ ...model, priority: index })),
        instructions,
        decision_model: useDecisionModel ? decisionModel : null,
        version: agent?.version || 0,
        is_draft: models.length === 0
      }
      if (agent) await api.agents.update(agent.id, payload)
      else await api.agents.create(payload)
      localStorage.removeItem(draftKey)
      if (saveTimeout) clearTimeout(saveTimeout as unknown as number)
      await onComplete?.()
    } catch (e: unknown) {
      if (saveTimeout) clearTimeout(saveTimeout as unknown as number)
      const msg = getErrorMessage(e)
      if (msg.includes('modified by another process')) error = 'This agent was modified elsewhere. Please refresh and try again.'
      else if (msg.includes('already exists')) error = 'An agent with this name already exists. Please choose a different name.'
      else error = msg
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
    models = [...models, { model_id: defaultModelId, priority: models.length, description: '', instructions: '' }]
  }
  function removeModel(index: number) {
    models = models.filter((_, i) => i !== index)
  }
  function moveUp(index: number) {
    if (index === 0) return
    const next = [...models]
    ;[next[index - 1], next[index]] = [next[index], next[index - 1]]
    models = next
  }
  function moveDown(index: number) {
    if (index === models.length - 1) return
    const next = [...models]
    ;[next[index], next[index + 1]] = [next[index + 1], next[index]]
    models = next
  }

  let allModelOptions = $derived(availableModels.map((m) => ({ value: m.full_model_id, label: `${m.provider_name} · ${m.display_name} · ${m.full_model_id}` })))
  let decisionOptions = $derived([{ value: '', label: 'Select a model...' }, ...allModelOptions])

  let canSaveAsDraft = $derived(name.trim() !== '')
  let canCreate = $derived(name.trim() !== '' && models.length > 0)
  let primaryLabel = $derived(agent ? 'Save' : 'Create agent')
</script>

<div class="agent-editor">
  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  {#if modelsLoadState === 'empty'}
    <div class="warning-banner">
      <span class="icon">warning</span>
      <div>
        <strong>No models available</strong>
        <p>Configure at least one provider with credentials to create agents.</p>
        <button class="btn btn-primary" onclick={goToProviders}><span class="icon">cloud</span>Go to Providers</button>
      </div>
    </div>
  {/if}

  <SectionCard title="Basic information">
    <div class="form-group">
      <label for="agent-name">Name <span class="required">*</span></label>
      <input id="agent-name" type="text" bind:value={name} placeholder="e.g., Research Assistant" />
    </div>
    <div class="form-group">
      <label for="agent-description">Description</label>
      <textarea id="agent-description" bind:value={description} rows={2} placeholder="Optional description of what this agent does"></textarea>
    </div>
  </SectionCard>

  <SectionCard title="Models" description="Models are tried in order (top = highest priority). Add descriptions to help the decision model choose.">
    {#snippet badge()}
      {#if models.length === 0}<span class="badge badge-yellow">Draft</span>{/if}
    {/snippet}
    {#if modelsLoadState === 'loading'}
      <div class="loading">Loading available models...</div>
    {:else if modelsLoadState === 'error'}
      <div class="error-box">
        <p>Failed to load models. Please try again.</p>
        <button class="btn btn-secondary" onclick={loadAvailableModels}>Reload</button>
      </div>
    {:else if models.length === 0}
      <div class="placeholder-inline">
        <p>No models added yet</p>
        {#if modelsLoadState === 'empty'}
          <p class="hint">No models are currently available. Configure providers first.</p>
        {:else}
          <button class="btn btn-secondary" onclick={addModel}><span class="icon">add</span>Add Model</button>
        {/if}
      </div>
    {:else}
      <div class="models-stack">
        {#each models as model, i (i)}
          <ModelCard
            {model}
            index={i}
            total={models.length}
            {availableModels}
            onChange={(next) => {
              const nextArr = [...models]
              nextArr[i] = next
              models = nextArr
            }}
            onMoveUp={() => moveUp(i)}
            onMoveDown={() => moveDown(i)}
            onDelete={() => removeModel(i)}
          />
        {/each}
      </div>
      <button class="btn btn-secondary" onclick={addModel}><span class="icon">add</span>Add Model</button>
    {/if}
  </SectionCard>

  <SectionCard title="General instructions">
    <div class="form-group">
      <label for="instructions-content">Instructions</label>
      <textarea
        id="instructions-content"
        bind:value={instructions.content}
        rows={4}
        placeholder="System instructions that apply to all models"
      ></textarea>
    </div>
    <SegmentedControl
      bind:value={instructions.injection}
      options={[
        { value: 'beginning', label: 'Inject at beginning' },
        { value: 'end', label: 'Inject at end' }
      ]}
      ariaLabel="Instruction injection position"
    />
  </SectionCard>

  <SectionCard title="Decision model (optional)" description="Use a cheap model to intelligently route requests based on context.">
    <Switch bind:checked={useDecisionModel} label="Enable decision-based routing" id="decision-toggle" />
    {#if useDecisionModel}
      {#if modelsLoadState === 'loading'}
        <div class="loading">Loading models...</div>
      {:else if modelsLoadState === 'empty'}
        <div class="placeholder-inline">
          <p class="hint">No models available for decision routing.</p>
          <button class="btn btn-primary" onclick={goToProviders}><span class="icon">cloud</span>Go to Providers</button>
        </div>
      {:else if modelsLoadState === 'loaded'}
        <div class="form-group">
          <label for="decision-model">Decision Model <span class="required">*</span></label>
          <Dropdown bind:value={decisionModel.model_id} options={decisionOptions} searchable={true} placeholder="Select a model..." />
        </div>
      {/if}
      <div class="form-group">
        <label for="decision-prompt">System Prompt <span class="required">*</span></label>
        <textarea
          id="decision-prompt"
          bind:value={decisionModel.system_prompt}
          rows={4}
          placeholder="You are a routing assistant. Choose the best model for the user's request based on complexity, cost, and requirements."
        ></textarea>
      </div>
    {/if}
  </SectionCard>

  <div class="editor-footer">
    <div class="footer-spacer"></div>
    <div class="footer-actions">
      <button class="btn btn-secondary" onclick={cancel} disabled={loading}>Cancel</button>
      <button class="btn btn-secondary" onclick={save} disabled={!canSaveAsDraft || loading}>Save as draft</button>
      <button class="btn btn-primary" onclick={save} disabled={!canCreate || loading}>{loading ? 'Saving…' : primaryLabel}</button>
    </div>
  </div>
</div>

<style>
  .agent-editor {
    display: flex;
    flex-direction: column;
    gap: 24px;
    padding-bottom: 24px;
  }

  .editor-footer {
    position: sticky;
    bottom: 0;
    z-index: 2;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 16px;
    margin-top: 8px;
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 12px;
    box-shadow: var(--shadow-sm);
  }

  .footer-actions {
    display: flex;
    gap: 12px;
    margin-left: auto;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .form-group label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
  }

  .required {
    color: var(--color-error-text);
  }

  .models-stack {
    display: flex;
    flex-direction: column;
    gap: 16px;
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
    background: var(--color-notification-warning-bg);
    border: 1px solid var(--color-notification-warning-border);
    border-radius: 8px;
  }

  .warning-banner .icon {
    font-size: 24px;
    color: var(--color-notification-warning-icon);
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

  .error-box {
    padding: 16px;
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    border-radius: 8px;
    text-align: center;
  }

  .error-box p {
    margin-bottom: 12px;
  }
</style>
