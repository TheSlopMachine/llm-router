<script lang="ts">
  import { Button, Picker, Banner, ContentUnavailable, TextField, Field, Text, VStack } from '@fuckyouryan/fui'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { Provider, UINode, ModalButton } from '$lib/types'
  import DynamicForm, { collectButtons, buttonAppearance } from '../../../components/domain/DynamicForm.svelte'
  import { t } from '$lib/i18n.svelte'

  let {
    provider,
    onComplete,
    closeModal,
    updateButtons,
  }: {
    provider: Provider
    onComplete: () => void
    closeModal: () => void
    updateButtons: (buttons: ModalButton[]) => void
  } = $props()

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
  // The key's dashboard name, set here instead of the automatic one. Auth
  // flows create the credential server-side: the name lands on it right after.
  let label = $state('')
  let knownIds: string[] = []

  async function providerCredentialIds(): Promise<string[]> {
    const all = (await api.credentials.list()) as { id: string; provider_id: string }[]
    return all.filter((c) => c.provider_id === provider.id).map((c) => c.id)
  }

  async function finish(): Promise<void> {
    const name = label.trim()
    if (name !== '') {
      try {
        const fresh = (await providerCredentialIds()).filter((id) => !knownIds.includes(id))
        for (const id of fresh) await api.credentials.update(id, { label: name })
      } catch {
        // the key exists; only its name stays automatic
      }
    }
    onComplete()
  }

  onMount(async (): Promise<void> => {
    try {
      knownIds = await providerCredentialIds()
    } catch {
      knownIds = []
    }
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
      const flow = await api.auth.initiate(provider.id)
      if (flow.status === 'render' && flow.nodes) {
        nodes = flow.nodes
        flowId = flow.flow_id ?? ''
        mode = 'wizard'
      } else if (flow.status === 'redirect' && flow.redirect_url) {
        window.open(flow.redirect_url, '_blank')
        redirectMessage = 'Complete authentication in the new window, then close this dialog.'
        mode = 'wizard'
        nodes = []
        flowId = flow.flow_id ?? ''
      } else if (flow.status === 'complete') {
        await finish()
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
    return mode === 'wizard' ? t('common.actions.continue') : t('common.actions.save')
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
        return { label: n.text || t('common.actions.cancel'), ...buttonAppearance(n), onclick: closeModal, disabled: loading }
      }
      return {
        label: n.text || defaultSubmitLabel(),
        ...buttonAppearance(n),
        onclick: () => submitFor(action),
        disabled: loading,
      }
    })
    if (!tree.some((n) => (n.form_action || 'submit') === 'cancel')) {
      buttons.push({ label: t('common.actions.cancel'), buttonStyle: 'neutral', onclick: closeModal, disabled: loading })
    }
    if (tree.length === 0 && nodes.length > 0) {
      buttons.push({
        label: defaultSubmitLabel(),
        buttonStyle: 'prominent',
        onclick: () => submitFor('submit'),
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
      const flow = await api.auth.step({
        provider_id: provider.id,
        flow_id: flowId,
        action,
        values: formValues,
      })
      if (flow.status === 'render' && flow.nodes) {
        nodes = flow.nodes
      } else if (flow.status === 'redirect' && flow.redirect_url) {
        window.open(flow.redirect_url, '_blank')
        redirectMessage = 'Complete authentication in the new window.'
      } else if (flow.status === 'complete') {
        await finish()
      } else {
        error = t('credentials.unexpected_auth')
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
      await api.credentials.create({ provider_id: provider.id, label: label.trim() || undefined, data: formValues })
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
      await api.credentials.create({ provider_id: provider.id, label: label.trim() || undefined, data })
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }
</script>

{#if error}
  <Banner tint="danger" text={error} />
{/if}

{#if manualAvailable}
  <div class="entry-switch">
    <Picker pickerStyle="segmented"
      value={entryTab}
      options={[
        { value: 'flow', label: t('credentials.device_login') },
        { value: 'manual', label: t('credentials.paste') },
      ]}
      ariaLabel="Credential entry method"
      onchange={(v) => switchEntry(v as 'flow' | 'manual')}
    />
  </div>
{/if}

<!-- one column, one step (space-4) between every block; the name field is
     built like the form's own fields (label over input, gap space-1) -->
<VStack gap={4}>
{#if mode !== 'loading'}
  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('credentials.name_label')}</Text>
    <TextField bind:value={label} placeholder={t('credentials.name_placeholder')} ariaLabel={t('credentials.name_label')} />
  </VStack>
{/if}

{#if mode === 'loading'}
  <ContentUnavailable title={t('common.state.loading')} />
{:else if mode === 'wizard'}
  {#if redirectMessage}
    <Banner tint="info" text={redirectMessage} />
  {/if}
  {#if nodes.length > 0}
    <DynamicForm {nodes} bind:values={formValues} busy={loading} flat />
  {/if}
{:else if mode === 'single'}
  <DynamicForm {nodes} bind:values={formValues} busy={loading} flat />
{:else}
  <p class="form-text">{t('credentials.no_form_hint')}</p>
  <Field label={t('credentials.json_label')}>
    <TextField maxRows={10} id="cred-raw" minRows={6} bind:value={rawJson} />
  </Field>
  <div class="form-actions">
    <Button buttonStyle="prominent" onclick={submitRaw} disabled={loading}>{t('common.actions.save')}</Button>
  </div>
{/if}
</VStack>

<style>
  .entry-switch {
    display: flex;
    justify-content: center;
    margin-bottom: var(--fui-space-4);
  }

  .form-text {
    font-size: var(--fui-text-md);
    color: var(--fui-color-text-soft);
  }

  .form-actions {
    display: flex;
    gap: var(--fui-space-3);
    margin-top: var(--fui-space-4);
  }
</style>
