<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal'
  import type { Provider, Credential, ModalButton } from '../lib/types'

  export let provider: Provider
  export let credentials: Credential[]
  export let onComplete: () => void
  export let onUpdate: () => void
  export let updateButtons: (buttons: ModalButton[]) => void
  export let updateTitle: (title: string) => void
  export let closeModal: () => void

  let view: 'list' | 'auth' = 'list'
  let authHtml: string = ''
  let flowId: string = ''
  let loading: boolean = false
  let error: string = ''

  onMount(() => {
    // If no credentials, automatically jump to auth flow
    if (credentials.length === 0) {
      switchToAuthFlow()
    } else {
      updateListButtons()
    }
  })

  function updateListButtons(): void {
    updateTitle(`${provider.name} Credentials`)
    updateButtons([
      { label: 'Cancel', variant: 'secondary', onClick: closeModal },
      { label: 'Add credential', variant: 'primary', onClick: switchToAuthFlow, loading }
    ])
  }

  function updateAuthButtons(): void {
    updateTitle(`Add Credential · ${provider.name}`)
    updateButtons([
      { label: 'Cancel', variant: 'secondary', onClick: closeModal }
    ])
  }

  async function switchToAuthFlow(): Promise<void> {
    loading = true
    error = ''
    updateListButtons()

    try {
      const res = await fetch('/api/llm-router/dashboard/auth/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider_id: provider.id })
      })

      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Failed to start auth flow')
      }

      const data = await res.json()

      if (data.status === 'render' && data.html) {
        flowId = data.flow_id
        authHtml = data.html
        view = 'auth'
        updateAuthButtons()
      } else if (data.status === 'redirect' && data.external_url) {
        window.open(data.external_url, '_blank')
        error = 'Please complete authentication in the new window'
      } else if (data.status === 'complete') {
        onComplete()
      } else {
        throw new Error('Unexpected auth flow response')
      }
    } catch (e) {
      error = (e as Error).message
    } finally {
      loading = false
      if (view === 'list') {
        updateListButtons()
      }
    }
  }

  async function submitAuthStep(e: Event): Promise<void> {
    e.preventDefault()
    const form = e.target as HTMLFormElement
    const formData = new FormData(form)

    loading = true
    error = ''

    try {
      const res = await fetch('/api/llm-router/dashboard/auth/callback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams(formData as any).toString()
      })

      if (!res.ok) {
        const errData = await res.json()
        throw new Error(errData.error || 'Auth step failed')
      }

      const data = await res.json()

      if (data.status === 'complete') {
        onComplete()
        return
      } else if (data.status === 'render' && data.html) {
        authHtml = data.html
      } else if (data.status === 'redirect' && data.external_url) {
        window.open(data.external_url, '_blank')
        error = 'Please complete authentication in the new window'
      } else {
        throw new Error('Unexpected auth flow response')
      }
    } catch (e) {
      error = (e as Error).message
    } finally {
      loading = false
    }
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
      credentials = credentials.filter(c => c.id !== id)
      if (onUpdate) onUpdate()
      
      // If no credentials left, jump to auth flow
      if (credentials.length === 0) {
        switchToAuthFlow()
      }
    } catch (e) {
      error = (e as Error).message
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
          <button 
            class="btn-icon" 
            on:click={() => deleteCredential(cred.id, cred.label)}
            aria-label="Delete credential">
            <span class="icon">delete</span>
          </button>
        </div>
      {/each}
    </div>
  {/if}
{:else}
  <div class="auth-html">
    <form on:submit={submitAuthStep}>
      <input type="hidden" name="flow_id" value={flowId} />
      {@html authHtml}
    </form>
  </div>
  {#if loading}
    <div class="empty-state">Processing…</div>
  {/if}
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

  :global(.auth-html form) {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  :global(.auth-html input[type="submit"]),
  :global(.auth-html button[type="submit"]) {
    background: var(--color-button-container);
    border: 1px solid var(--color-outline-light);
    color: var(--color-text-on-button);
    cursor: pointer;
    padding: 0 16px;
    height: 32px;
    border-radius: 12px;
    font-size: 14px;
    font-weight: 500;
    width: auto;
    transition: background 0.15s;
  }

  :global(.auth-html input[type="submit"]:hover),
  :global(.auth-html button[type="submit"]:hover) {
    background: var(--color-button-container-high);
  }
</style>
