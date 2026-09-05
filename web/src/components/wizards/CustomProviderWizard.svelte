<script lang="ts">
  import { api } from '../../lib/api'
  import Dropdown from '../Dropdown.svelte'
  import type { ModalButton } from '../../lib/types'

  let {
    adapterTypes,
    onComplete,
    updateButtons,
    closeModal
  } = $props<{
    adapterTypes: string[]
    onComplete: () => void
    updateButtons: (buttons: ModalButton[]) => void
    closeModal: () => void
  }>()

  let name: string = $state('')
  let typeKey: string = $state('')
  let baseURL: string = $state('')
  let creating: boolean = $state(false)
  let error: string = $state('')

  $effect(() => {
    if (adapterTypes.length && !typeKey) {
      typeKey = adapterTypes[0]
    }
  })

  $effect(() => {
    void name
    void typeKey
    void creating
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
      await (api.providers.create as any)({ name, type: typeKey, base_url: baseURL })
      onComplete()
    } catch (e) {
      error = (e as Error).message
      creating = false
      updateFormButtons()
    }
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
    options={adapterTypes.map((t: string) => ({value: t, label: t}))}
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
