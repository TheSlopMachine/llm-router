<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import Dropdown from '../Dropdown.svelte'
  import type { ModalButton } from '../../lib/types'

  export let adapterTypes: string[]
  export let onComplete: () => void
  
  export let updateButtons: (buttons: ModalButton[]) => void
  export let closeModal: () => void

  let name: string = ''
  let typeKey: string = ''
  let baseURL: string = ''
  let creating: boolean = false
  let error: string = ''

  onMount(() => {
    if (adapterTypes.length && !typeKey) {
      typeKey = adapterTypes[0]
    }
    updateFormButtons()
  })

  function updateFormButtons(): void {
    updateButtons([
      { label: 'Cancel', variant: 'secondary', onClick: closeModal },
      { 
        label: 'Add', 
        variant: 'primary', 
        onClick: create,
        disabled: !name || !typeKey,
        loading: creating
      }
    ])
  }

  async function create(): Promise<void> {
    if (!name || !typeKey) return
    
    creating = true
    error = ''
    updateFormButtons()
    
    try {
      await api.providers.create({ name, type: typeKey, base_url: baseURL })
      onComplete()
    } catch (e) {
      error = (e as Error).message
      creating = false
      updateFormButtons()
    }
  }

  $: {
    name
    typeKey
    creating
    updateFormButtons()
  }
</script>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

<div class="form-group">
  <label for="provider-name">Name</label>
  <input id="provider-name" type="text" bind:value={name} placeholder="OpenAI Production" />
</div>

<div class="form-group">
  <label for="provider-type">Type</label>
  <Dropdown
    bind:value={typeKey}
    options={adapterTypes.map(t => ({value: t, label: t}))}
    placeholder="Select provider type"
  />
</div>

<div class="form-group">
  <label for="base-url">Base URL</label>
  <input id="base-url" type="text" bind:value={baseURL} placeholder="https://api.openai.com" />
</div>

<style>
  .form-group {
    margin-bottom: 16px;
  }

  .form-group:last-child {
    margin-bottom: 0;
  }
</style>
