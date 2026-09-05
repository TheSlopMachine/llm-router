<script lang="ts">
  import { onMount } from 'svelte'
  import { untrack } from 'svelte'
  import { api } from '../../lib/api'
  import Dropdown from '../Dropdown.svelte'
  import type { ModalButton } from '../../lib/types'

  let {
    adapterTypes: initialTypes,
    editingProvider = null,
    onComplete,
    updateButtons,
    closeModal
  } = $props<{
    adapterTypes?: string[]
    editingProvider?: any | null
    onComplete: () => void
    updateButtons: (buttons: ModalButton[]) => void
    closeModal: () => void
  }>()

  let name: string = $state(editingProvider?.name ?? '')
  let typeKey: string = $state(editingProvider?.type ?? '')
  let baseURL: string = $state(editingProvider?.base_url ?? '')
  let creating: boolean = $state(false)
  let error: string = $state('')
  let types: string[] = $state(initialTypes ? [...initialTypes] : [])
  let hasFetched = $state(false)

  let options = $derived(types.map((t: string) => ({ value: t, label: t })))

  function ensureDefaultType(): void {
    if (types.length && !typeKey) typeKey = types[0]
  }

  function syncButtons(): void {
    const isEdit = !!editingProvider
    untrack(() =>
      updateButtons([
        { label: 'Cancel', variant: 'secondary', onClick: closeModal },
        {
          label: isEdit ? 'Save' : 'Add',
          variant: 'primary',
          onClick: create,
          disabled: !name.trim() || (!typeKey && types.length > 0),
          loading: creating
        }
      ])
    )
  }

  onMount(() => {
    ensureDefaultType()
    if (types.length === 0 && !hasFetched) {
      hasFetched = true
      api.providers
        .adapterTypes()
        .then((fetched: string[]) => {
          if (Array.isArray(fetched) && fetched.length) {
            types = fetched
            ensureDefaultType()
            syncButtons()
          }
        })
        .catch(() => {
          hasFetched = false
        })
    }
    syncButtons()
  })

  // Keep default type when types arrive — guarded to avoid loop.
  $effect(() => {
    if (types.length && !typeKey) {
      untrack(() => {
        typeKey = types[0]
      })
    }
  })

  // Sync if parent later supplies initialTypes
  $effect(() => {
    if (initialTypes?.length && types.length === 0 && !hasFetched) {
      untrack(() => {
        types = [...initialTypes!]
        ensureDefaultType()
        syncButtons()
      })
    }
  })

  $effect(() => {
    void name
    void typeKey
    void creating
    void types.length
    untrack(() => syncButtons())
  })

  async function create(): Promise<void> {
    if (!name.trim()) return
    creating = true
    error = ''
    syncButtons()

    try {
      if (editingProvider) {
        const rawId = String(editingProvider.id ?? editingProvider.ID ?? '')
        const id = rawId.replace(/^custom:/, '') || rawId.replace('custom:', '')
        await (api.providers.update as any)(id || rawId.replace('custom:', ''), {
          name: name.trim(),
          base_url: baseURL.trim(),
          icon_url: editingProvider.icon_url ?? editingProvider.IconURL ?? ''
        })
      } else {
        const payload: any = { name: name.trim(), base_url: baseURL.trim() }
        if (typeKey) payload.type = typeKey
        await (api.providers.create as any)(payload)
      }
      onComplete()
    } catch (e) {
      error = (e as Error).message
      creating = false
      syncButtons()
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
    options={options}
    placeholder={types.length ? "Select provider type" : "Loading types…"}
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
