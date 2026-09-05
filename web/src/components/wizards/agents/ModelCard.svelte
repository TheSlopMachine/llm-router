<script lang="ts">
  import Dropdown from '../../Dropdown.svelte'
  import type { AgentModel, AvailableModel } from '../../../lib/types'

  let {
    model,
    index,
    total,
    availableModels,
    onChange,
    onMoveUp,
    onMoveDown,
    onDelete
  } = $props<{
    model: AgentModel
    index: number
    total: number
    availableModels: AvailableModel[]
    onChange: (next: AgentModel) => void
    onMoveUp: () => void
    onMoveDown: () => void
    onDelete: () => void
  }>()

  let isAvailable = $derived(availableModels.some((m: AvailableModel) => m.full_model_id === model.model_id))

  let options = $derived.by(() => {
    const base = availableModels.map((m: AvailableModel) => ({
      value: m.full_model_id,
      label: `${m.provider_name} · ${m.display_name} · ${m.full_model_id}`
    }))
    if (!isAvailable && model.model_id) {
      const fallback = availableModels.find((m: AvailableModel) => m.full_model_id === model.model_id)
      const label = fallback
        ? `${fallback.provider_name} · ${fallback.display_name} · ${model.model_id}`
        : `${model.model_id} (unavailable)`
      base.push({ value: model.model_id, label })
    }
    return base
  })
</script>

<div class="model-card">
  <div class="model-card-header">
    <span class="priority-badge">{index + 1}</span>
    <div class="model-picker">
      <Dropdown
        value={model.model_id}
        onchange={(v) => onChange({ ...model, model_id: v })}
        {options}
        searchable={true}
        placeholder="Select a model"
      />
    </div>
    <div class="model-actions">
      <button class="btn-icon" onclick={onMoveUp} disabled={index === 0} title="Move up" aria-label="Move up">
        <span class="icon">arrow_upward</span>
      </button>
      <button class="btn-icon" onclick={onMoveDown} disabled={index === total - 1} title="Move down" aria-label="Move down">
        <span class="icon">arrow_downward</span>
      </button>
      <button class="btn-icon btn-danger" onclick={onDelete} disabled={total === 1} title="Remove" aria-label="Remove">
        <span class="icon">delete</span>
      </button>
    </div>
  </div>

  {#if !isAvailable && model.model_id}
    <div class="warning-text">This model is no longer available.</div>
  {/if}

  <div class="form-group">
    <label for={`model-desc-${index}`}>Description (for decision model)</label>
    <input
      id={`model-desc-${index}`}
      type="text"
      bind:value={
        () => model.description,
        (v) => onChange({ ...model, description: v })
      }
      placeholder="e.g., Best for complex reasoning and analysis"
    />
  </div>

  <div class="form-group">
    <label for={`model-instr-${index}`}>Model-specific instructions (optional)</label>
    <textarea
      id={`model-instr-${index}`}
      bind:value={
        () => model.instructions,
        (v) => onChange({ ...model, instructions: v })
      }
      rows={2}
      placeholder="Additional instructions for this specific model"
    ></textarea>
  </div>
</div>

<style>
  .model-card {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    border: 1px solid var(--color-outline-soft);
    border-radius: 12px;
    background: var(--color-surface);
  }

  .model-card-header {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .priority-badge {
    width: 24px;
    height: 24px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-size: 12px;
    font-weight: 600;
    color: var(--color-text-soft);
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    flex-shrink: 0;
  }

  .model-picker {
    flex: 1;
    min-width: 0;
  }

  .model-actions {
    display: flex;
    gap: 4px;
    flex-shrink: 0;
  }

  .model-actions .btn-icon {
    width: 32px;
    height: 32px;
    border-radius: 8px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    color: var(--color-text-soft);
  }

  .model-actions .btn-icon:hover:not(:disabled) {
    background: var(--color-hover-bg);
    color: var(--color-text);
  }

  .model-actions .btn-icon:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .model-actions .btn-icon.btn-danger {
    color: var(--color-error-text);
    border-color: var(--color-outline-light);
  }

  .warning-text {
    font-size: 12px;
    color: #ca8a04;
  }

  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
  }

  .form-group label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
  }
</style>
