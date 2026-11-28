<script lang="ts">
  // Body of the Add registry login modal. Rendered by the shared Modal shell.
  import { Banner, Field, Form, ModalChrome, TextField } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'

  let {
    onComplete,
    closeModal,
  }: {
    onComplete: () => void | Promise<void>
    closeModal: () => void
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
</script>

<ModalChrome buttons={[
  { label: t('common.actions.cancel'), buttonStyle: 'neutral', onclick: closeModal, disabled: saving },
  { label: saving ? t('common.actions.saving') : t('common.actions.add'), buttonStyle: 'prominent', onclick: save, disabled: saving || !complete },
]} />

{#if error}
  <Banner tint="danger" text={error} />
{/if}
<Form>
  <Field label={t('settings.docker.registry_host.label')}>
    <TextField bind:value={host} placeholder="ghcr.io" autocomplete="off" />
  </Field>
  <Field label={t('settings.docker.registry_username.label')}>
    <TextField bind:value={username} autocomplete="off" />
  </Field>
  <Field label={t('settings.docker.registry_secret.label')}>
    <TextField bind:value={secret} secure onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') void save() }} />
  </Field>
</Form>
