<script lang="ts">
  import { onMount } from 'svelte'
  import { untrack } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import type { ModalButton } from '../../lib/types'

  let {
    editingProvider = null,
    onComplete,
    updateButtons,
    closeModal
  } = $props<{
    editingProvider?: any | null
    onComplete: () => void
    updateButtons: (buttons: ModalButton[]) => void
    closeModal: () => void
  }>()

  let name: string = $state(editingProvider?.name ?? '')
  let baseURL: string = $state(editingProvider?.base_url ?? '')
  let iconURL: string = $state(editingProvider?.icon_url ?? editingProvider?.IconURL ?? '')
  let creating: boolean = $state(false)
  let error: string = $state('')

  function syncButtons(): void {
    const isEdit = !!editingProvider
    untrack(() =>
      updateButtons([
        { label: 'Cancel', variant: 'secondary', onClick: closeModal },
        {
          label: isEdit ? 'Save' : 'Add',
          variant: 'primary',
          onClick: create,
          disabled: !name.trim() || !baseURL.trim(),
          loading: creating
        }
      ])
    )
  }

  onMount(() => {
    syncButtons()
  })

  $effect(() => {
    void name
    void baseURL
    void iconURL
    void creating
    untrack(() => syncButtons())
  })

  async function create(): Promise<void> {
    if (!name.trim() || !baseURL.trim()) return
    creating = true
    error = ''
    syncButtons()

    try {
      if (editingProvider) {
        const rawId = String(editingProvider.id ?? editingProvider.ID ?? '')
        const id = rawId.replace(/^custom:/, '')
        await api.providers.update(id, {
          name: name.trim(),
          base_url: baseURL.trim(),
          icon_url: iconURL.trim()
        })
      } else {
        await api.providers.create({
          name: name.trim(),
          base_url: baseURL.trim(),
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
</script>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

<div class="form-group">
  <label for="provider-name">Name *</label>
  <input id="provider-name" type="text" bind:value={name} placeholder="My LLM Provider" />
  <small>A friendly name for this provider</small>
</div>

<div class="form-group">
  <label for="base-url">Base URL *</label>
  <input id="base-url" type="text" bind:value={baseURL} placeholder="https://api.example.com/v1" />
  <small>OpenAI-compatible endpoint (must support /chat/completions)</small>
</div>

<div class="form-group">
  <label for="icon-url">Icon URL (Optional)</label>
  <input id="icon-url" type="text" bind:value={iconURL} placeholder="https://example.com/icon.svg" />
  <small>Optional icon for the provider card</small>
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
