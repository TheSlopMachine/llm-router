<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import type { Provider, Credential, ModalButton, ModalMenu } from '../lib/types'
  import CredentialWizard from './CredentialWizard.svelte'

  let {
    provider,
    credentials = $bindable([]),
    onComplete,
    onUpdate,
    onEdit = undefined,
    onDelete = undefined,
    updateButtons,
    updateMenu,
    updateTitle,
    closeModal
  } = $props<{
    provider: Provider
    credentials: Credential[]
    onComplete: () => void
    onUpdate: () => void
    onEdit?: () => void
    onDelete?: () => void
    updateButtons: (buttons: ModalButton[]) => void
    updateMenu: (menu: ModalMenu | null) => void
    updateTitle: (title: string) => void
    closeModal: () => void
  }>()

  let view = $state<'list' | 'auth'>('list')
  let error = $state('')

  let dropdownActions = $derived([
    { id: 'add', label: 'Add credential', icon: 'add' },
    ...(onEdit ? [{ id: 'edit', label: 'Edit Provider', icon: 'edit' }] : []),
    ...(onDelete ? [{ id: 'delete', label: 'Delete Provider', icon: 'delete', danger: true }] : []),
  ])

  onMount(() => {
    updateListButtons()
  })

  function updateListButtons(): void {
    updateTitle(`${provider.name} Credentials`)
    updateButtons([
      { label: 'Cancel', variant: 'secondary', onClick: closeModal },
    ])
    if (dropdownActions.length === 1) {
      const single = dropdownActions[0]
      updateButtons([
        { label: 'Cancel', variant: 'secondary', onClick: closeModal },
        { label: single.label, variant: 'primary', onClick: () => handleDropdownAction(single.id) },
      ])
      updateMenu(null)
    } else {
      updateMenu({ label: 'Actions', actions: dropdownActions, onaction: handleDropdownAction })
    }
  }

  function switchToAuthFlow(): void {
    view = 'auth'
    updateAuthButtons()
  }

  function updateAuthButtons(): void {
    updateTitle(`Add Credential · ${provider.name}`)
    updateButtons([{ label: 'Cancel', variant: 'secondary', onClick: closeModal }])
    updateMenu(null)
  }

  function handleDropdownAction(id: string): void {
    if (id === 'add') {
      switchToAuthFlow()
    } else if (id === 'edit') {
      onEdit?.()
    } else if (id === 'delete') {
      onDelete?.()
    }
  }

  function backToList(): void {
    view = 'list'
    updateListButtons()
  }

  async function deleteCredential(id: string, label: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Delete credential',
      message: `Are you sure you want to delete credential "${label}"? This action cannot be undone.`,
      severity: 'medium',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true
    })

    if (!confirmed) return

    try {
      await api.credentials.delete(id)
      credentials = credentials.filter((c: Credential) => c.id !== id)
      if (onUpdate) onUpdate()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }
</script>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if view === 'list'}
  {#if credentials.length === 0}
    <div class="empty-state">No credentials added yet</div>
  {:else}
    <div class="credentials-list">
      {#each credentials as cred}
        <div class="credential-item">
          <div class="credential-info">
            <span class="credential-label">{cred.label || 'Unnamed'}</span>
            {#if cred.is_expired}
              <span class="badge badge-red">Expired</span>
            {:else}
              <span class="badge badge-green">Active</span>
            {/if}
          </div>
          <button class="btn-icon" onclick={() => deleteCredential(cred.id, cred.label)} aria-label="Delete credential">
            <span class="icon">delete</span>
          </button>
        </div>
      {/each}
    </div>
  {/if}
{:else}
  <CredentialWizard {provider} onComplete={onComplete} onBack={backToList} />
{/if}

<style>
  .empty-state {
    padding: 32px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }

  .credentials-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .credential-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border: 1px solid var(--color-outline-soft);
    border-radius: 8px;
    transition: background 0.15s;
  }

  .credential-item:hover {
    background: var(--color-hover-bg);
  }

  .credential-info {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .credential-label {
    font-size: 14px;
    color: var(--color-text);
  }
</style>
