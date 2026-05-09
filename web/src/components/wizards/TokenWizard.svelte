<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import type { Token, Provider, ProviderModels, ModalButton } from '../../lib/types'

  export let providers: Provider[]
  export let editingToken: Token | null = null
  export let onComplete: (result: { token?: string }) => void
  
  export let updateButtons: (buttons: ModalButton[]) => void
  export let updateTitle: (title: string) => void
  export let closeModal: () => void

  let wizardStep: number = 1
  let wizardLoading: boolean = false
  let error: string = ''

  let tokenName: string = ''
  let selectedProviders: Set<string> = new Set()
  let providerModels: ProviderModels[] = []
  let selectedModels: Set<string> = new Set()
  let allowAll: boolean = false

  onMount(() => {
    if (editingToken) {
      tokenName = editingToken.name
      selectedModels = new Set(editingToken.rules?.allowed_models || [])
      allowAll = selectedModels.size === 0

      const types = new Set([...selectedModels].map((m) => m.split('/')[0]))
      selectedProviders = new Set(
        providers.filter((p) => types.has(p.type)).map((p) => p.id)
      )
    }

    updateStepButtons()
  })

  function updateStepButtons(): void {
    const title = editingToken ? 'Edit token' : 'New token'
    
    if (wizardStep === 1) {
      updateTitle(`${title} · Step 1 of 2`)
      updateButtons([
        { 
          label: 'Cancel', 
          variant: 'secondary', 
          onClick: closeModal 
        },
        { 
          label: 'Next', 
          variant: 'primary', 
          onClick: goToModels,
          disabled: !tokenName.trim() || selectedProviders.size === 0,
          loading: wizardLoading
        }
      ])
    } else {
      updateTitle(`${title} · Step 2 of 2`)
      updateButtons([
        { 
          label: 'Back', 
          variant: 'secondary', 
          onClick: goBackToStep1
        },
        { 
          label: editingToken ? 'Update token' : 'Create token', 
          variant: 'primary', 
          onClick: submit,
          loading: wizardLoading
        }
      ])
    }
  }

  function goBackToStep1(): void {
    wizardStep = 1
    error = ''
    updateStepButtons()
  }

  function toggleProvider(id: string): void {
    const s = new Set(selectedProviders)
    s.has(id) ? s.delete(id) : s.add(id)
    selectedProviders = s
    updateStepButtons()
  }

  async function goToModels(): Promise<void> {
    error = ''
    
    if (!tokenName.trim()) { 
      error = 'Token name is required.'
      return 
    }
    
    if (selectedProviders.size === 0) { 
      error = 'Select at least one provider.'
      return 
    }
    
    wizardLoading = true
    updateStepButtons()
    
    try {
      const result = await api.models.list([...selectedProviders])
      providerModels = result.providers || []
      const availableTypes = new Set(providerModels.map((p) => p.provider_type))
      for (const m of selectedModels) {
        const t = m.split('/')[0]
        if (!availableTypes.has(t)) selectedModels.delete(m)
      }
      selectedModels = new Set(selectedModels)
      wizardStep = 2
      error = ''
    } catch (e) {
      error = (e as Error).message
    } finally {
      wizardLoading = false
      updateStepButtons()
    }
  }

  function toggleModel(fullId: string): void {
    const s = new Set(selectedModels)
    s.has(fullId) ? s.delete(fullId) : s.add(fullId)
    selectedModels = s
  }

  async function submit(): Promise<void> {
    error = ''
    wizardLoading = true
    updateStepButtons()
    
    const allowed = allowAll ? null : [...selectedModels]
    const payload = {
      name: tokenName,
      rules: { allowed_models: allowed },
    }
    
    try {
      if (editingToken) {
        await api.tokens.update(editingToken.id, payload)
        onComplete({})
      } else {
        const result = await api.tokens.create(payload)
        onComplete({ token: result.token })
      }
    } catch (e) {
      error = (e as Error).message
      wizardLoading = false
      updateStepButtons()
    }
  }

  $: if (wizardStep === 1) {
    updateStepButtons()
  }
</script>

{#if wizardStep === 1}
  <div class="form-group">
    <label for="token-name">Token name</label>
    <input id="token-name" type="text" bind:value={tokenName} placeholder="My Application" />
    {#if error && !tokenName.trim()}
      <span class="field-error">{error}</span>
    {/if}
  </div>

  <div class="form-group">
    <div class="form-label">Providers</div>
    <div class="checkbox-list">
      {#each providers as p}
        <label class="checkbox-item">
          <input type="checkbox" checked={selectedProviders.has(p.id)} on:change={() => toggleProvider(p.id)} />
          <span>{p.name} <span class="text-muted">({p.type})</span></span>
        </label>
      {/each}
    </div>
    {#if error && selectedProviders.size === 0}
      <span class="field-error">{error}</span>
    {/if}
  </div>

{:else if wizardStep === 2}
  {#if error}
    <div class="error-msg" style="color: var(--color-error-text);">{error}</div>
  {/if}

  <div class="form-group">
    <label class="checkbox-item">
      <input type="checkbox" bind:checked={allowAll} />
      <span>Allow all models</span>
    </label>
  </div>

  {#if !allowAll}
    <div class="model-sections">
      {#each providerModels as pm}
        <div class="model-section">
          <div class="section-label">
            {pm.provider_name}
            {#if pm.error}
              <span class="badge badge-red">{pm.error}</span>
            {/if}
          </div>
          {#if pm.models?.length}
            <div class="checkbox-list">
              {#each pm.models as model}
                {@const fullId = `${pm.provider_type}/${model}`}
                <label class="checkbox-item">
                  <input type="checkbox" checked={selectedModels.has(fullId)} on:change={() => toggleModel(fullId)} />
                  <span class="mono">{model}</span>
                </label>
              {/each}
            </div>
          {:else if !pm.error}
            <div class="empty-state">No models available</div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
{/if}

<style>
  .form-group {
    margin-bottom: 16px;
  }

  .form-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
    margin-bottom: 6px;
  }

  .checkbox-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 8px 16px;
    margin-top: 8px;
  }

  .checkbox-item {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    cursor: pointer;
    user-select: none;
  }

  .checkbox-item input[type="checkbox"] {
    width: auto;
    cursor: pointer;
  }

  .text-muted {
    color: var(--color-text-soft);
  }

  .field-error {
    display: block;
    margin-top: 6px;
    font-size: 12px;
    color: #dc2626;
  }

  .model-sections {
    display: flex;
    flex-direction: column;
    gap: 24px;
    margin-top: 16px;
  }

  .model-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-label {
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text);
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .empty-state {
    padding: 16px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }
</style>
