<script lang="ts">
  import { api } from '../../lib/api'
  import type { Provider } from '../../lib/types'

  export let editingProvider: Provider | null = null
  export let onComplete: (result: any) => void

  let name = editingProvider?.name || ''
  let baseURL = editingProvider?.base_url || ''
  let iconURL = editingProvider?.icon_url || ''
  let error = ''
  let loading = false

  async function save() {
    loading = true
    error = ''
    try {
      if (editingProvider) {
        // Extract the ID without 'custom:' prefix
        const id = editingProvider.id.replace('custom:', '')
        await api.providers.update(id, { name, base_url: baseURL, icon_url: iconURL })
      } else {
        await api.providers.create({ name, base_url: baseURL, icon_url: iconURL })
      }
      onComplete({})
    } catch (e) {
      error = (e as Error).message
    } finally {
      loading = false
    }
  }

  function handleKeyPress(e: KeyboardEvent) {
    if (e.key === 'Enter' && !loading && name && baseURL) {
      save()
    }
  }

  $: isValid = name.trim() !== '' && baseURL.trim() !== ''
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
      on:keypress={handleKeyPress}
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
      on:keypress={handleKeyPress}
    />
    <small>OpenAI-compatible endpoint (must support /chat/completions)</small>
  </div>
  
  <div class="form-group">
    <label for="iconURL">Icon URL (Optional)</label>
    <input 
      type="url" 
      id="iconURL" 
      bind:value={iconURL} 
      placeholder="https://example.com/icon.svg"
      on:keypress={handleKeyPress}
    />
    <small>Optional icon for the provider card</small>
  </div>
  
  <div class="actions">
    <button 
      class="btn btn-primary" 
      on:click={save} 
      disabled={loading || !isValid}
    >
      {loading ? 'Saving...' : editingProvider ? 'Update Provider' : 'Create Provider'}
    </button>
  </div>
</div>

<style>
  .wizard {
    padding: 24px;
  }

  .form-group {
    margin-bottom: 20px;
  }

  .form-group label {
    display: block;
    margin-bottom: 8px;
    font-weight: 500;
    font-size: 14px;
  }

  .form-group input {
    width: 100%;
    padding: 10px 12px;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    font-size: 14px;
    background: var(--color-bg);
    color: var(--color-text);
  }

  .form-group input:focus {
    outline: none;
    border-color: var(--color-primary);
    box-shadow: 0 0 0 3px var(--color-primary-alpha);
  }

  .form-group small {
    display: block;
    margin-top: 6px;
    font-size: 12px;
    color: var(--color-text-soft);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    margin-top: 24px;
    padding-top: 20px;
    border-top: 1px solid var(--color-border);
  }

  .error-msg {
    margin-bottom: 20px;
    padding: 12px;
    background: var(--color-error-bg);
    color: var(--color-error);
    border-radius: 6px;
    font-size: 14px;
  }
</style>
