<script lang="ts">
  // Body of the Add repository modal. Rendered by the shared Modal shell.
  import { Banner, Field, Form, ModalChrome, TextField } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import type { PluginRepo } from '$lib/types'

  let {
    onAdded,
    onComplete,
    closeModal,
  }: {
    onAdded: (repo: PluginRepo) => void
    onComplete: () => void
    closeModal: () => void
  } = $props()

  let url = $state('')
  let adding = $state(false)
  let error = $state('')

  async function add(): Promise<void> {
    if (!url.trim() || adding) return
    adding = true
    error = ''
    try {
      onAdded(await api.repos.addRepo(url.trim()))
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      adding = false
    }
  }
</script>

<ModalChrome buttons={[
  { label: t('common.actions.cancel'), buttonStyle: 'neutral', onclick: closeModal, disabled: adding },
  { label: t('common.actions.add'), buttonStyle: 'prominent', onclick: add, disabled: !url.trim(), loading: adding },
]} />

{#if error}
  <Banner tint="danger" text={error} />
{/if}
<Form>
  <Field label={t('plugins.repo.url_label')} hint={t('plugins.repo.url_hint')}>
    <TextField bind:value={url} placeholder="https://github.com/..." autocomplete="off" onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') void add() }} />
  </Field>
</Form>
