<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import type { ModalButton, Provider, UINode } from '../../lib/types'
  import DynamicForm from '../ui/DynamicForm.svelte'

  let {
    editingProvider = null,
    onComplete,
    updateButtons,
    closeModal
  } = $props<{
    editingProvider?: Provider | null
    onComplete: () => void
    updateButtons: (buttons: ModalButton[]) => void
    closeModal: () => void
  }>()

  let types = $state<string[]>([])
  let typeKey = $state('custom')
  let name = $state('')
  let qualifier = $state('')
  let iconURL = $state('')
  let configNodes = $state<UINode[] | null>(null)
  let configValues = $state<Record<string, unknown>>({})
  let rawConfig = $state('{}')
  let useRawConfig = $state(false)
  let creating = $state(false)
  let error = $state('')

  const isEdit = $derived(!!editingProvider)

  onMount(async (): Promise<void> => {
    try {
      types = await api.providers.adapterTypes()
    } catch (e) {
      error = getErrorMessage(e)
    }
    if (editingProvider) {
      name = editingProvider.name ?? ''
      typeKey = editingProvider.type_key || editingProvider.type || 'custom'
      qualifier = editingProvider.qualifier ?? ''
      iconURL = editingProvider.icon_url ?? ''
      configValues = { ...(editingProvider.config ?? {}) }
      if (typeKey === 'custom' && editingProvider.base_url) {
        configValues = { ...configValues, base_url: editingProvider.base_url }
      }
    }
    await loadConfigSchema()
    syncButtons()
  })

  async function loadConfigSchema(): Promise<void> {
    configNodes = null
    useRawConfig = false
    if (!typeKey || typeKey === 'agents') return
    try {
      const schema = await api.providers.configSchemaForType(typeKey)
      if (schema.nodes) {
        configNodes = schema.nodes
      } else {
        useRawConfig = true
        rawConfig = JSON.stringify(configValues, null, 2)
      }
    } catch (e) {
      error = getErrorMessage(e)
      useRawConfig = true
    }
  }

  async function onTypeChange(next: string): Promise<void> {
    typeKey = next
    configValues = {}
    await loadConfigSchema()
    syncButtons()
  }

  function syncButtons(): void {
    updateButtons([
      { label: 'Cancel', variant: 'secondary', onClick: closeModal },
      {
        label: isEdit ? 'Save' : 'Add',
        variant: 'primary',
        onClick: save,
        disabled: !name.trim() || creating,
        loading: creating
      }
    ])
  }

  function collectConfig(): Record<string, unknown> | null {
    if (useRawConfig) {
      try {
        return JSON.parse(rawConfig) as Record<string, unknown>
      } catch {
        error = 'Config is not valid JSON'
        return null
      }
    }
    return { ...configValues }
  }

  async function save(): Promise<void> {
    if (!name.trim()) return
    creating = true
    error = ''
    syncButtons()
    try {
      const config = collectConfig()
      if (config === null) {
        creating = false
        syncButtons()
        return
      }
      if (editingProvider) {
        await api.providers.updateInstance(editingProvider.id, {
          name: name.trim(),
          config,
          icon_url: iconURL.trim()
        })
      } else {
        await api.providers.createInstance({
          name: name.trim(),
          type_key: typeKey,
          qualifier: qualifier.trim() || undefined,
          config,
          icon_url: iconURL.trim() || undefined
        })
      }
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
      creating = false
      syncButtons()
    }
  }

  function onConfigSubmit(_action: string, formValues: Record<string, unknown>): void {
    configValues = { ...formValues }
    void save()
  }
</script>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if !isEdit}
  <div class="form-group">
    <label for="provider-type">Type *</label>
    <select id="provider-type" value={typeKey} onchange={(e) => onTypeChange((e.target as HTMLSelectElement).value)}>
      {#each types as t}
        <option value={t}>{t}</option>
      {/each}
    </select>
  </div>
{/if}

<div class="form-group">
  <label for="provider-name">Name *</label>
  <input id="provider-name" type="text" bind:value={name} placeholder="My LLM Provider" oninput={syncButtons} />
</div>

{#if !isEdit && typeKey !== 'custom' && typeKey !== 'agents'}
  <div class="form-group">
    <label for="provider-qualifier">Qualifier (optional)</label>
    <input id="provider-qualifier" type="text" bind:value={qualifier} placeholder="eu" />
    <small>Distinguishes multiple providers of the same type</small>
  </div>
{/if}

{#if configNodes}
  <DynamicForm nodes={configNodes} bind:values={configValues} onSubmit={onConfigSubmit} busy={creating} submitLabel={isEdit ? 'Save' : 'Add'} />
{:else if useRawConfig}
  <div class="form-group">
    <label for="provider-config">Config JSON</label>
    <textarea id="provider-config" rows="5" bind:value={rawConfig} autocomplete="off"></textarea>
  </div>
{/if}

<div class="form-group">
  <label for="icon-url">Icon URL (optional)</label>
  <input id="icon-url" type="text" bind:value={iconURL} placeholder="https://example.com/icon.svg" />
</div>

<style>
  .form-group {
    margin-bottom: 16px;
  }
  .form-group:last-child {
    margin-bottom: 0;
  }
  .form-group small {
    display: block;
    margin-top: 6px;
    font-size: 12px;
    color: var(--color-text-soft);
  }
</style>
