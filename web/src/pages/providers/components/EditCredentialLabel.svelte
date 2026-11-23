<script lang="ts">
  // Body of the "Edit Key" modal. Rendered by the shared Modal shell.
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import type { Credential } from '$lib/types'
  import { Banner, Field, ModalChrome, TextField, VStack } from '@fuckyouryan/fui'

  let {
    cred,
    onComplete,
    closeModal,
  }: {
    cred: Credential
    onComplete: () => void | Promise<void>
    closeModal: () => void
  } = $props()

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
</script>

<ModalChrome buttons={[
  { label: t('common.actions.cancel'), buttonStyle: 'neutral', onclick: closeModal, disabled: saving },
  { label: saving ? t('common.actions.saving') : t('common.actions.save'), buttonStyle: 'prominent', onclick: save, disabled: saving },
]} />

<VStack gap={4}>
  {#if error}
    <Banner tint="danger" text={error} />
  {/if}
  <Field label={t('common.labels.name')}>
    <TextField
      id="cred-edit-label"
      bind:value={label}
      placeholder={t('credentials.label_example')}
      onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') void save() }}
    />
  </Field>
</VStack>
