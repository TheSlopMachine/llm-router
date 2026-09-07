<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import type { Provider, UINode } from '../lib/types'
  import DynamicForm from './ui/DynamicForm.svelte'

  let {
    provider,
    onComplete,
    onBack,
  } = $props<{
    provider: Provider
    onComplete: () => void
    onBack?: () => void
  }>()

  type Mode = 'loading' | 'wizard' | 'single' | 'raw'
  let mode: Mode = $state('loading')
  let nodes = $state<UINode[]>([])
  let flowId = $state('')
  let rawJson = $state('{}')
  let loading = $state(false)
  let error = $state('')
  let redirectMessage = $state('')

  onMount(async (): Promise<void> => {
    try {
      const res = await api.auth.initiate(provider.id)
      if (res.status === 'render' && res.nodes) {
        nodes = res.nodes
        flowId = res.flow_id ?? ''
        mode = 'wizard'
      } else if (res.status === 'redirect' && res.redirect_url) {
        window.open(res.redirect_url, '_blank')
        redirectMessage = 'Complete authentication in the new window, then close this dialog.'
        mode = 'wizard'
        nodes = []
        flowId = res.flow_id ?? ''
      } else if (res.status === 'complete') {
        onComplete()
      } else {
        await loadSingleStep()
      }
    } catch (e) {
      const message = getErrorMessage(e)
      if (message.includes('stepped auth flows') || message.includes('409')) {
        await loadSingleStep()
      } else {
        error = message
        mode = 'single'
      }
    }
  })

  async function loadSingleStep(): Promise<void> {
    try {
      const schema = await api.providers.credentialSchema(provider.id)
      if (schema.nodes) {
        nodes = schema.nodes
        mode = 'single'
      } else {
        mode = 'raw'
      }
    } catch (e) {
      error = getErrorMessage(e)
      mode = 'raw'
    }
  }

  async function submitWizard(action: string, formValues: Record<string, unknown>): Promise<void> {
    loading = true
    error = ''
    try {
      const res = await api.auth.step({
        provider_id: provider.id,
        flow_id: flowId,
        action,
        values: formValues,
      })
      if (res.status === 'render' && res.nodes) {
        nodes = res.nodes
      } else if (res.status === 'redirect' && res.redirect_url) {
        window.open(res.redirect_url, '_blank')
        redirectMessage = 'Complete authentication in the new window.'
      } else if (res.status === 'complete') {
        onComplete()
      } else {
        error = 'Unexpected auth response'
      }
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function submitSingle(action: string, formValues: Record<string, unknown>): Promise<void> {
    if (action === 'cancel' || action === 'restart') {
      onBack?.()
      return
    }
    loading = true
    error = ''
    try {
      await api.credentials.create({ provider_id: provider.id, data: formValues })
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function submitRaw(): Promise<void> {
    loading = true
    error = ''
    try {
      const data = JSON.parse(rawJson) as Record<string, unknown>
      await api.credentials.create({ provider_id: provider.id, data })
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }
</script>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if mode === 'loading'}
  <div class="empty-state">Loading…</div>
{:else if mode === 'wizard'}
  {#if redirectMessage}
    <div class="banner banner-info">{redirectMessage}</div>
  {/if}
  {#if nodes.length > 0}
    <DynamicForm {nodes} onSubmit={submitWizard} busy={loading} submitLabel="Continue" />
  {/if}
  {#if onBack}
    <div class="form-actions">
      <button class="btn btn-secondary" onclick={onBack}>Back</button>
    </div>
  {/if}
{:else if mode === 'single'}
  <DynamicForm {nodes} onSubmit={submitSingle} busy={loading} submitLabel="Save" />
  {#if onBack}
    <div class="form-actions">
      <button class="btn btn-secondary" onclick={onBack}>Back</button>
    </div>
  {/if}
{:else}
  <p class="form-text">This provider type has no credential form. Paste credential data as JSON.</p>
  <div class="form-group">
    <label for="cred-raw">Credential JSON</label>
    <textarea id="cred-raw" rows="6" bind:value={rawJson} autocomplete="off"></textarea>
  </div>
  <div class="form-actions">
    {#if onBack}<button class="btn btn-secondary" onclick={onBack}>Back</button>{/if}
    <button class="btn btn-primary" disabled={loading} onclick={submitRaw}>Save</button>
  </div>
{/if}

<style>
  .empty-state {
    padding: 32px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }
  .banner {
    padding: 10px 12px;
    border-radius: 8px;
    font-size: 13px;
    margin-bottom: 12px;
  }
  .banner-info {
    background: var(--color-info-bg, #eef4ff);
  }
  .form-text {
    font-size: 14px;
    color: var(--color-text-soft);
  }
  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 12px;
  }
  .form-group label {
    font-size: 13px;
    font-weight: 500;
  }
  .form-actions {
    display: flex;
    gap: 8px;
    margin-top: 12px;
  }
</style>
