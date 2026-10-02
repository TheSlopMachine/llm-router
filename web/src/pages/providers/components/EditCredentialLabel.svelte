<script lang="ts">
  // Body of the "Edit Key" modal. Rendered by the shared Modal (title, close
  // button, footer chrome), replacing a hand-rolled dialog in
  // ProviderDetailPage whose .modal-* classes had no CSS left.
  import { untrack } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import type { Credential, ModalButton } from '$lib/types'
  import { Text, TextEdit, VStack } from '$ui'

  let {
    cred,
    onComplete,
    closeModal,
    updateButtons,
  } = $props<{
    cred: Credential
    onComplete: () => void | Promise<void>
    closeModal: () => void
    updateButtons: (buttons: ModalButton[]) => void
  }>()

  // initial value only: the modal owns this component's lifetime
  // svelte-ignore state_referenced_locally
  let label = $state(cred.label || '')
  let saving = $state(false)
  let error = $state('')

  async function save(): Promise<void> {
    saving = true
    error = ''
    try {
      await api.credentials.update(cred.id, { label: label.trim() })
      await onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      saving = false
    }
  }

  $effect(() => {
    void saving
    untrack(() => {
      updateButtons([
        { label: t('Cancel'), variant: 'secondary', onClick: closeModal, disabled: saving },
        { label: saving ? t('Saving…') : t('Save'), variant: 'primary', onClick: () => void save(), disabled: saving },
      ])
    })
  })
</script>

<VStack gap={4}>
  {#if error}
    <Text tone="danger" size="sm">{error}</Text>
  {/if}
  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('Name')}</Text>
    <TextEdit
      id="cred-edit-label"
      bind:value={label}
      hint={t('e.g. Work account')}
      onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') void save() }}
    />
  </VStack>
</VStack>
