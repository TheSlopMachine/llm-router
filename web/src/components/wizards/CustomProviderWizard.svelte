<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import { t } from '../../lib/i18n.svelte'
  import type { ModalButton, Provider } from '../../lib/types'
  import TextEdit from '../ui/controls/TextEdit.svelte'
  import Text from '../ui/controls/Text.svelte'
  import VStack from '../ui/layout/VStack.svelte'

  let {
    editingProvider = null,
    onComplete,
    updateButtons,
    closeModal
  } = $props<{
    editingProvider?: Provider | null
    onComplete: () => void
    updateButtons: (buttons: ModalButton[]) => void
    closeModal: () => void
  }>()

  let name = $state('')
  let baseURL = $state('')
  let iconURL = $state('')
  let creating = $state(false)
  let error = $state('')

  const isEdit = $derived(!!editingProvider)

  onMount((): void => {
    if (editingProvider) {
      name = editingProvider.name ?? ''
      const cfg = (editingProvider.config ?? {}) as Record<string, unknown>
      const fromConfig = typeof cfg.base_url === 'string' ? cfg.base_url : ''
      baseURL = fromConfig || editingProvider.base_url || ''
      iconURL = editingProvider.icon_url ?? ''
    }
    syncButtons()
  })

  function syncButtons(): void {
    updateButtons([
      {
        label: isEdit ? t('common.actions.save') : t('common.actions.add'),
        variant: 'primary',
        onClick: save,
        disabled: !name.trim() || !baseURL.trim() || creating,
        loading: creating,
      },
      { label: t('common.actions.cancel'), variant: 'secondary', onClick: closeModal },
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
        const cfg = { ...((editingProvider.config ?? {}) as Record<string, unknown>), base_url: trimmedBase }
        await api.providers.updateInstance(editingProvider.id, {
          name: name.trim(),
          config: cfg,
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
    <div class="error-msg">{error}</div>
  {/if}

  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('common.labels.name')} *</Text>
    <TextEdit
      id="provider-name"
      bind:value={name}
      hint={t('providers.fields.my_provider')}
      onchange={syncButtons}
    />
  </VStack>

  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('providers.fields.base_url')} *</Text>
    <TextEdit
      id="provider-base-url"
      bind:value={baseURL}
      hint="https://api.example.com/v1"
      onchange={syncButtons}
    />
    <Text size="sm" tone="soft">{t('providers.fields.endpoint_details')}</Text>
  </VStack>

  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('providers.fields.icon_url')} ({t('common.labels.optional')})</Text>
    <TextEdit
      id="icon-url"
      bind:value={iconURL}
      hint="https://example.com/icon.svg"
    />
  </VStack>
</VStack>
