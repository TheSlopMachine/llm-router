<script lang="ts">
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import { t } from '../../lib/i18n.svelte'
  import type { Provider } from '../../lib/types'
  import { Banner, Field, ModalChrome, TextField, VStack } from '@fuckyouryan/fui'

  let {
    editingProvider = null,
    onComplete,
    closeModal
  }: {
    editingProvider?: Provider | null
    onComplete: () => void
    closeModal: () => void
  } = $props()

  // Initial values only: the modal owns this component's lifetime.
  // svelte-ignore state_referenced_locally
  const providerConfig = (editingProvider?.config ?? {}) as Record<string, unknown>
  // svelte-ignore state_referenced_locally
  let name = $state(editingProvider?.name ?? '')
  // svelte-ignore state_referenced_locally
  let baseURL = $state((typeof providerConfig.base_url === 'string' ? providerConfig.base_url : '') || editingProvider?.base_url || '')
  // svelte-ignore state_referenced_locally
  let iconURL = $state(editingProvider?.icon_url ?? '')
  let saving = $state(false)
  let error = $state('')

  const complete = $derived(!!name.trim() && !!baseURL.trim())

  async function save(): Promise<void> {
    if (!complete || saving) return
    saving = true
    error = ''
    try {
      const trimmedBase = baseURL.trim()
      if (editingProvider) {
        await api.providers.updateInstance(editingProvider.id, {
          name: name.trim(),
          config: { ...providerConfig, base_url: trimmedBase },
          icon_url: iconURL.trim()
        })
      } else {
        await api.providers.createInstance({
          name: name.trim(),
          type_key: 'custom',
          config: { base_url: trimmedBase },
          icon_url: iconURL.trim() || undefined
        })
      }
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      saving = false
    }
  }
</script>

<ModalChrome buttons={[
  { label: t('common.actions.cancel'), buttonStyle: 'neutral', onclick: closeModal, disabled: saving },
  { label: editingProvider ? t('common.actions.save') : t('common.actions.add'), buttonStyle: 'prominent', onclick: save, disabled: !complete || saving, loading: saving },
]} />

<VStack gap={4}>
  {#if error}
    <Banner tint="danger" text={error} />
  {/if}

  <Field label={t('common.labels.name')} required>
    <TextField id="provider-name" bind:value={name} placeholder={t('providers.fields.my_provider')} />
  </Field>

  <Field label={t('providers.fields.base_url')} hint={t('providers.fields.endpoint_details')} required>
    <TextField id="provider-base-url" bind:value={baseURL} placeholder="https://api.example.com/v1" />
  </Field>

  <Field label={t('providers.fields.icon_url')}>
    <TextField id="icon-url" bind:value={iconURL} placeholder="https://example.com/icon.svg" />
  </Field>
</VStack>
