<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import type { Provider, ModalButton } from '../../lib/types'

  export let editingProvider: Provider | null = null
  export let onComplete: (result: any) => void
  export let updateButtons: (buttons: ModalButton[]) => void = () => {}
  export let updateTitle: (title: string) => void = () => {}
  export let closeModal: () => void = () => {}

  let name = editingProvider?.name || ''
  let baseURL = editingProvider?.base_url || ''
  let iconURL = editingProvider?.icon_url || ''
  let error = ''
  let loading = false

  $: isValid = name.trim() !== '' && baseURL.trim() !== ''

  onMount(() => {
    updateTitle(editingProvider ? 'Edit Provider' : 'New Provider')
    updateFormButtons()
  })

  function updateFormButtons(): void {
    updateButtons([
      { label: 'Cancel', variant: 'secondary', onClick: closeModal },
      {
        label: loading ? 'Saving...' : editingProvider ? 'Update Provider' : 'Create Provider',
        variant: 'primary',
        onClick: save,
        disabled: !isValid,
        loading
      }
    ])
  }

  async function save(): Promise<void> {
    if (!isValid || loading) return
    loading = true
    error = ''
    updateFormButtons()
    try {
      if (editingProvider) {
        const id = editingProvider.id.replace('custom:', '')
        await api.providers.update(id, { name, base_url: baseURL, icon_url: iconURL })
      } else {
        await api.providers.create({ name, base_url: baseURL, icon_url: iconURL })
      }
      onComplete({})
    } catch (e) {
      error = (e as Error).message
      loading = false
      updateFormButtons()
    }
  }

  function handleKeyDown(e: KeyboardEvent): void {
    if (e.key === 'Enter' && isValid && !loading) {
      e.preventDefault()
      save()
    }
  }

  $: {
    void isValid; void loading
    updateFormButtons()
  }
</script>

<div class="wizard">
  {#if error}
    <div class="error-msg">{error}</div>
  {/if}
  
  <div class="form-group">
    <label for="name">Name *</label>
    <input 
      type="text" 
      id="name" 
      bind:value={name} 
      placeholder="My LLM Provider"
      on:keydown={handleKeyDown}
    />
    <small>A friendly name for this provider</small>
  </div>
  
  <div class="form-group">
    <label for="baseURL">Base URL *</label>
    <input 
      type="url" 
      id="baseURL" 
      bind:value={baseURL} 
      placeholder="https://api.example.com/v1"
      on:keydown={handleKeyDown}
    />
    <small>OpenAI-compatible endpoint</small>
  </div>
  
  <div class="form-group">
    <label for="iconURL">Icon URL (Optional)</label>
    <input 
      type="url" 
      id="iconURL" 
      bind:value={iconURL} 
      placeholder="https://example.com/icon.svg"
      on:keydown={handleKeyDown}
    />
    <small>Optional icon for the provider</small>
  </div>
</div>

<style>
  .wizard {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .form-group {
    margin-bottom: 16px;
  }

  .form-group:last-child {
    margin-bottom: 0;
  }

  .form-group small {
    display: block;
    margin-top: 2px;
    font-size: 12px;
    line-height: 16px;
    color: var(--color-text-disabled);
  }

  .error-msg {
    margin-bottom: 16px;
    padding: 12px 16px;
    background: var(--color-badge-red-bg);
    color: var(--color-error-text);
    border: 1px solid var(--color-badge-red-bg);
    border-radius: 8px;
    font-size: 14px;
  }
</style>
