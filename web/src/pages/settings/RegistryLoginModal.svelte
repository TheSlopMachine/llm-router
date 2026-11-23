<script lang="ts">
  // Body of the Add registry login modal. Rendered by the shared Modal shell.
  import { untrack } from 'svelte'
  import { Banner, Field, TextField, VStack } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import type { ModalButton } from '$lib/types'

  let {
    onComplete,
    closeModal,
    updateButtons,
  }: {
    onComplete: () => void | Promise<void>
    closeModal: () => void
    updateButtons: (buttons: ModalButton[]) => void
  } = $props()

  let host = $state('')
  let username = $state('')
  let secret = $state('')
  let saving = $state(false)
  let error = $state('')

  let complete = $derived(!!host && !!username && !!secret)

  async function save(): Promise<void> {
    if (!complete || saving) return
    saving = true
    error = ''
    try {
      await api.docker.saveRegistry(host, username, secret)
      await onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      saving = false
    }
  }

  $effect(() => {
    void saving
    void complete
    untrack(() => {
      updateButtons([
        { label: t('common.actions.cancel'), buttonStyle: 'neutral', onclick: closeModal, disabled: saving },
        {
          label: saving ? t('common.actions.saving') : t('common.actions.add'),
          buttonStyle: 'prominent',
          onclick: () => void save(),
          disabled: saving || !complete,
        },
      ])
    })
  })
</script>

<VStack gap={4}>
  {#if error}
    <Banner tint="danger" text={error} />
  {/if}
  <Field label={t('settings.docker.registry_host.label')}>
    <TextField bind:value={host} placeholder="ghcr.io" size="lg" autocomplete="off" />
  </Field>
  <Field label={t('settings.docker.registry_username.label')}>
    <TextField bind:value={username} size="lg" autocomplete="off" />
  </Field>
  <Field label={t('settings.docker.registry_secret.label')}>
    <TextField bind:value={secret} secure size="lg" onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') void save() }} />
  </Field>
</VStack>
