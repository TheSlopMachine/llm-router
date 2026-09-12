<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import type { Provider, UINode, ModalButton } from '../lib/types'
  import DynamicForm, { collectButtons, buttonVariant } from './ui/DynamicForm.svelte'
  import SegmentedControl from './ui/SegmentedControl.svelte'
  import { t } from '../lib/i18n.svelte'

  let {
    provider,
    onComplete,
    closeModal,
    updateButtons,
  } = $props<{
    provider: Provider
    onComplete: () => void
    closeModal: () => void
    updateButtons: (buttons: ModalButton[]) => void
  }>()

  type Mode = 'loading' | 'wizard' | 'single' | 'raw'
  let mode: Mode = $state('loading')
  let nodes = $state<UINode[]>([])
  let formValues = $state<Record<string, unknown>>({})
  let flowId = $state('')
  let rawJson = $state('{}')
  let loading = $state(false)
  let error = $state('')
  let redirectMessage = $state('')
  // Providers with both a stepped auth flow and a manual credential form
  // (e.g. Kiro) let the user pick the entry method up front.
  let entryTab = $state<'flow' | 'manual'>('flow')
  let manualAvailable = $state(false)

  onMount(async (): Promise<void> => {
    if (provider.supports_auth_flow) {
      try {
        const schema = await api.providers.credentialSchema(provider.id)
        manualAvailable = !!(schema.nodes && schema.nodes.length > 0)
      } catch {
        manualAvailable = false
      }
    }
    if (!provider.supports_auth_flow) {
      await loadSingleStep()
      return
    }
    await loadFlow()
  })

  async function switchEntry(tab: 'flow' | 'manual'): Promise<void> {
    if (tab === entryTab) return
    entryTab = tab
    formValues = {}
    redirectMessage = ''
    if (tab === 'manual') {
      await loadSingleStep()
    } else {
      await loadFlow()
    }
  }

  async function loadFlow(): Promise<void> {
    mode = 'loading'
    error = ''
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
        return
      } else {
        await loadSingleStep()
        return
      }
    } catch (e) {
      const message = getErrorMessage(e)
      if (message.includes('stepped auth flows') || message.includes('409')) {
        await loadSingleStep()
        return
      } else {
        error = message
        mode = 'single'
      }
    }
    syncFooter()
  }

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
    syncFooter()
  }

  // Footer owns every flow control: tree-declared buttons first, then an
  // auto Cancel (unless the tree declares cancel itself), then a default
  // primary submit when the tree declares no buttons at all.
  function defaultSubmitLabel(): string {
    return mode === 'wizard' ? 'Continue' : 'Save'
  }

  function submitFor(action: string): void {
    if (action === 'cancel') {
      closeModal()
      return
    }
    if (mode === 'single') {
      if (action === 'restart') {
        closeModal()
        return
      }
      void submitSingle(action, { ...formValues })
      return
    }
    void submitWizard(action, { ...formValues })
  }

  function syncFooter(): void {
    const tree = collectButtons(nodes)
    const buttons: ModalButton[] = tree.map((n) => {
      const action = n.form_action || 'submit'
      if (action === 'cancel') {
        return { label: n.text || 'Cancel', variant: buttonVariant(n), onClick: closeModal, disabled: loading }
      }
      return {
        label: n.text || defaultSubmitLabel(),
        variant: buttonVariant(n),
        onClick: () => submitFor(action),
        disabled: loading,
      }
    })
    if (!tree.some((n) => (n.form_action || 'submit') === 'cancel')) {
      buttons.push({ label: 'Cancel', variant: 'secondary', onClick: closeModal, disabled: loading })
    }
    if (tree.length === 0 && nodes.length > 0) {
      buttons.push({
        label: defaultSubmitLabel(),
        variant: 'primary',
        onClick: () => submitFor('submit'),
        disabled: loading,
      })
    }
    updateButtons(buttons)
  }

  async function submitWizard(action: string, formValues: Record<string, unknown>): Promise<void> {
    loading = true
    error = ''
    syncFooter()
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
        error = t('Unexpected auth response')
      }
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
      syncFooter()
    }
  }

  async function submitSingle(action: string, formValues: Record<string, unknown>): Promise<void> {
    if (action === 'cancel' || action === 'restart') {
      closeModal()
      return
    }
    loading = true
    error = ''
    syncFooter()
    try {
      await api.credentials.create({ provider_id: provider.id, data: formValues })
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
      syncFooter()
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

{#if manualAvailable}
  <div class="entry-switch">
    <SegmentedControl
      value={entryTab}
      options={[
        { value: 'flow', label: t('Device login') },
        { value: 'manual', label: t('Paste tokens') },
      ]}
      ariaLabel="Credential entry method"
      onchange={(v) => switchEntry(v as 'flow' | 'manual')}
    />
  </div>
{/if}

{#if mode === 'loading'}
  <div class="empty-state">{t('Loading…')}</div>
{:else if mode === 'wizard'}
  {#if redirectMessage}
    <div class="banner banner-info">{redirectMessage}</div>
  {/if}
  {#if nodes.length > 0}
    <DynamicForm {nodes} bind:values={formValues} busy={loading} />
  {/if}
{:else if mode === 'single'}
  <DynamicForm {nodes} bind:values={formValues} busy={loading} />
{:else}
  <p class="form-text">{t('This provider type has no credential form. Paste credential data as JSON.')}</p>
  <div class="form-group">
    <label for="cred-raw">{t('Credential JSON')}</label>
    <textarea id="cred-raw" rows="6" bind:value={rawJson} autocomplete="off"></textarea>
  </div>
  <div class="form-actions">
    <button class="btn btn-primary" disabled={loading} onclick={submitRaw}>{t('Save')}</button>
  </div>
{/if}

<style>
  .entry-switch {
    display: flex;
    justify-content: center;
    margin-bottom: 16px;
  }
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
