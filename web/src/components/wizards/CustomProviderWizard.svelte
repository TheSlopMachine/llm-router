<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import { t } from '../../lib/i18n.svelte'
  import type { ModalButton, Provider } from '../../lib/types'
  import { TextField, Text, VStack, Banner } from '@fuckyouryan/fui'

  let {
    editingProvider = null,
    onComplete,
    updateButtons,
    closeModal
  }: {
    editingProvider?: Provider | null
    onComplete: () => void
    updateButtons: (buttons: ModalButton[]) => void
    closeModal: () => void
  } = $props()

  let name = $state('')
  let baseURL = $state('')
  let iconURL = $state('')
  let creating = $state(false)
  let error = $state('')

  const isEdit = $derived(!!editingProvider)

  onMount((): void => {
    if (editingProvider) {
      name = editingProvider.name ?? ''
      const providerConfig = (editingProvider.config ?? {}) as Record<string, unknown>
      const fromConfig = typeof providerConfig.base_url === 'string' ? providerConfig.base_url : ''
      baseURL = fromConfig || editingProvider.base_url || ''
      iconURL = editingProvider.icon_url ?? ''
    }
    syncButtons()
  })

  function syncButtons(): void {
    updateButtons([
      {
        label: isEdit ? t('common.actions.save') : t('common.actions.add'),
        buttonStyle: 'prominent',
        onClick: save,
        disabled: !name.trim() || !baseURL.trim() || creating,
        loading: creating,
      },
      { label: t('common.actions.cancel'), buttonStyle: 'neutral', onClick: closeModal },
    ])
  }

  async function save(): Promise<void> {
    if (!name.trim() || !baseURL.trim()) return
    creating = true
    error = ''
    syncButtons()
    try {
      const trimmedBase = baseURL.trim()
      if (editingProvider) {
        const updatedConfig = { ...((editingProvider.config ?? {}) as Record<string, unknown>), base_url: trimmedBase }
        await api.providers.updateInstance(editingProvider.id, {
          name: name.trim(),
          config: updatedConfig,
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
      creating = false
      syncButtons()
    }
  }
</script>

<VStack gap={4}>
  {#if error}
    <Banner tint="danger" text={error} />
  {/if}

  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('common.labels.name')} *</Text>
    <TextField
      id="provider-name"
      bind:value={name}
      hint={t('providers.fields.my_provider')}
      onchange={syncButtons}
    />
  </VStack>

  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('providers.fields.base_url')} *</Text>
    <TextField
      id="provider-base-url"
      bind:value={baseURL}
      hint="https://api.example.com/v1"
      onchange={syncButtons}
    />
    <Text size="sm" foregroundStyle="secondary">{t('providers.fields.endpoint_details')}</Text>
  </VStack>

  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('providers.fields.icon_url')} ({t('common.labels.optional')})</Text>
    <TextField
      id="icon-url"
      bind:value={iconURL}
      hint="https://example.com/icon.svg"
    />
  </VStack>
</VStack>
