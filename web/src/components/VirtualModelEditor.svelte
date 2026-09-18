<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import { t } from '../lib/i18n.svelte'
  import Dropdown from './Dropdown.svelte'
  import Button from './ui/Button.svelte'
  import SectionCard from './ui/SectionCard.svelte'
  import { squircle } from '../lib/squircle'
  import CapabilityChips from './ui/CapabilityChips.svelte'
  import type { VirtualModel, AvailableModel } from '../lib/types'

  let {
    vm,
    onComplete,
    onCancel
  } = $props<{
    vm?: VirtualModel
    onComplete?: (() => void | Promise<void>) | undefined
    onCancel?: (() => void | Promise<void>) | undefined
  }>()

  let name = $state('')
  let description = $state('')
  let instruction = $state('')
  let models = $state<string[]>([])
  let hydratedFor = $state<string | null>(null)

  let availableModels = $state<AvailableModel[]>([])
  let modelsLoadState = $state<'loading' | 'loaded' | 'empty' | 'error'>('loading')
  let loading = $state(false)
  let error = $state('')

  $effect(() => {
    const id = vm?.id ?? 'new'
    if (vm && hydratedFor !== id) {
      name = vm.name ?? ''
      description = vm.description ?? ''
      instruction = vm.instruction ?? ''
      models = (vm.models ?? []).map((m: { model_id: string }) => m.model_id)
      hydratedFor = id
    }
  })

  onMount(() => {
    void loadAvailableModels()
  })

  async function loadAvailableModels() {
    try {
      availableModels = await api.virtualModels.availableModels()
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

  async function save() {
    if (!canSave || loading) return
    loading = true
    error = ''
    try {
      const payload = {
        name: name.trim(),
        description: description.trim(),
        instruction,
        models: models.map((model_id) => ({ model_id })),
        version: vm?.version || 0,
      }
      if (vm) await api.virtualModels.update(vm.id, payload)
      else await api.virtualModels.create(payload)
      await onComplete?.()
    } catch (e: unknown) {
      const msg = getErrorMessage(e)
      if (msg.includes('modified by another process')) error = t('This virtual model was modified elsewhere. Please refresh and try again.')
      else if (msg.includes('already exists')) error = t('A virtual model with this name already exists. Please choose a different name.')
      else error = msg
      loading = false
    }
  }

  function addModel() {
    models = [...models, availableModels[0]?.full_model_id ?? '']
  }
  function removeModel(index: number) {
    models = models.filter((_, i) => i !== index)
  }
  function setModel(index: number, id: string) {
    const next = [...models]
    next[index] = id
    models = next
  }

  // Drag to reorder — same pattern as the credential pool.
  let dragIndex = $state(-1)

  function onDragStart(e: DragEvent, index: number) {
    dragIndex = index
    e.dataTransfer?.setData('text/plain', String(index))
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function onDragOver(e: DragEvent) {
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }
  function onDrop(e: DragEvent, target: number) {
    e.preventDefault()
    const from = dragIndex >= 0 ? dragIndex : Number(e.dataTransfer?.getData('text/plain') ?? -1)
    dragIndex = -1
    if (from < 0 || from === target) return
    const next = [...models]
    const [moved] = next.splice(from, 1)
    next.splice(target, 0, moved)
    models = next
  }

  function infoOf(id: string): AvailableModel | undefined {
    return availableModels.find((m) => m.full_model_id === id)
  }

  // Aggregated view: capabilities = intersection, limits = minima (mirrors
  // what the backend recomputes on save).
  let aggregates = $derived.by(() => {
    const infos = models.map(infoOf).filter((m): m is AvailableModel => !!m)
    if (infos.length === 0) return { caps: [] as string[], context: 0, output: 0 }
    const count = new Map<string, number>()
    for (const m of infos) for (const c of m.capabilities ?? []) count.set(c, (count.get(c) ?? 0) + 1)
    const caps = [...count.entries()].filter(([, n]) => n === infos.length).map(([c]) => c).sort()
    const min = (pick: (m: AvailableModel) => number | undefined) =>
      infos.reduce((acc, m) => {
        const v = pick(m)
        return v && v > 0 && (acc === 0 || v < acc) ? v : acc
      }, 0)
    return { caps, context: min((m) => m.context_window), output: min((m) => m.max_tokens) }
  })

  let modelOptions = $derived(availableModels.map((m) => ({ value: m.full_model_id, label: m.display_name })))
  let canSave = $derived(name.trim() !== '' && models.length > 0 && models.every((id) => id !== ''))
</script>

<div class="vm-editor">
  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  {#if modelsLoadState === 'empty'}
    <div class="warning-banner">
      <span class="icon">warning</span>
      <div>
        <strong>{t('No models available')}</strong>
        <p>{t('Configure at least one provider with credentials to create virtual models.')}</p>
        <Button text={t('Go to Providers')} style="prominent" icon={{ name: 'cloud' }} onclick={() => { window.location.hash = '#/providers' }} />
      </div>
    </div>
  {/if}

  <SectionCard title="Basic information">
    <div class="form-group">
      <label for="vm-name">ID <span class="required">*</span></label>
      <input id="vm-name" type="text" bind:value={name} placeholder={t('How it appears in the model list — lowercase, hyphenated')} use:squircle={12} />
    </div>
    <div class="form-group">
      <label for="vm-description">{t('Description')}</label>
      <textarea id="vm-description" bind:value={description} rows={2} placeholder={t('Pretty name, e.g. Gemini fallback')} use:squircle={12}></textarea>
    </div>
    <div class="form-group">
      <label for="vm-instruction">{t('System instruction')}</label>
      <textarea
        id="vm-instruction"
        bind:value={instruction}
        rows={4}
        placeholder={t('Additional guidance for the LLM (behavioral, stylistic)')}
        use:squircle={12}
      ></textarea>
    </div>
  </SectionCard>

  <SectionCard title="Models">
    {#if modelsLoadState === 'loading'}
      <div class="loading">{t('Loading available models...')}</div>
    {:else if modelsLoadState === 'error'}
      <div class="error-box">
        <p>{t('Failed to load models. Please try again.')}</p>
        <Button text={t('Reload')} onclick={loadAvailableModels} />
      </div>
    {:else if models.length === 0}
      {#if modelsLoadState === 'empty'}
        <div class="placeholder-inline">
          <p class="hint">{t('No models are currently available. Configure providers first.')}</p>
        </div>
      {:else}
        <div class="add-row">
          <Button text={t('Add model')} icon={{ name: 'add' }} onclick={addModel} />
        </div>
      {/if}
    {:else}
      <div class="models-stack">
        {#each models as id, i (i)}
          <div
            class="model-row"
            draggable="true"
            ondragstart={(e) => onDragStart(e, i)}
            ondragover={onDragOver}
            ondrop={(e) => onDrop(e, i)}
            role="listitem"
          >
            <span class="col-priority">
              <span class="icon drag-handle" title={t('Drag to reorder')}>drag_indicator</span>
              {i + 1}
            </span>
            <Dropdown value={id} options={modelOptions} searchable={true} placeholder={t('Select a model...')} onchange={(v) => setModel(i, v)} />
            <span class="row-caps">
              <CapabilityChips caps={infoOf(id)?.capabilities ?? []} />
            </span>
            <span class="row-ctx">
              {#if infoOf(id)?.context_window}<span>{(infoOf(id)!.context_window! / 1000).toFixed(0)}k ctx</span>{/if}
              {#if infoOf(id)?.max_tokens}<span>{(infoOf(id)!.max_tokens! / 1000).toFixed(0)}k out</span>{/if}
            </span>
            <Button style="icon" danger icon={{ name: 'delete' }} ariaLabel={t('Remove model')} onclick={() => removeModel(i)} />
          </div>
        {/each}
      </div>
      <div class="add-row">
        <Button text={t('Add model')} icon={{ name: 'add' }} onclick={addModel} />
      </div>
      <div class="agg">
        <span class="agg-label">{t('Virtual model capabilities')}</span>
        <span class="agg-chips">
          {#if aggregates.caps.length > 0}
            <CapabilityChips caps={aggregates.caps} />
          {:else}
            <span class="agg-none">—</span>
          {/if}
        </span>
        {#if aggregates.context > 0}
          <span class="ctx-text" title={t('Smallest context window across the models')}>{(aggregates.context / 1000).toFixed(0)}k ctx</span>
        {/if}
        {#if aggregates.output > 0}
          <span class="ctx-text" title={t('Smallest max output across the models')}>{(aggregates.output / 1000).toFixed(0)}k out</span>
        {/if}
      </div>
    {/if}
  </SectionCard>

  <div class="editor-footer">
    <Button text={t('Cancel')} style="text" onclick={() => onCancel?.()} disabled={loading} />
    <Button
      text={loading ? t('Saving…') : vm ? t('Save') : t('Create virtual model')}
      style="prominent"
      disabled={!canSave || loading}
      onclick={save}
    />
  </div>
</div>

<style>
  .vm-editor {
    display: flex;
    flex-direction: column;
    gap: 24px;
    padding-bottom: 24px;
  }

  .editor-footer {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
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
    gap: 8px;
    margin-bottom: 12px;
  }

  .model-row {
    display: grid;
    grid-template-columns: 56px minmax(220px, 1fr) auto auto auto;
    align-items: center;
    gap: 12px;
  }

  .model-row[draggable='true'] {
    cursor: grab;
  }
  .model-row[draggable='true']:active {
    cursor: grabbing;
  }

  .col-priority {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--color-text-soft);
  }

  .drag-handle {
    font-size: 18px;
    color: var(--color-text-disabled);
  }

  .row-caps {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .row-ctx {
    display: flex;
    flex-direction: column;
    font-size: 12px;
    color: var(--color-text-soft);
    text-align: right;
    line-height: 16px;
  }

  .add-row :global(.btn) {
    width: 100%;
  }

  .agg {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-top: 12px;
  }

  .agg-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
  }

  .agg-chips {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }

  .agg-none {
    color: var(--color-text-disabled);
  }

  .ctx-text {
    font-size: 12px;
    color: var(--color-text-soft);
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
    background: var(--color-notification-error-bg);
    border: 1px solid var(--color-notification-error-border);
    color: var(--color-notification-error-text);
    border-radius: 8px;
    text-align: center;
  }

  .error-box p {
    margin-bottom: 12px;
  }
</style>
