<script lang="ts">
  // Body of the New/Edit custom pool modal. Rendered by the shared Modal shell.
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import type { ProxyPool } from '$lib/types'
  import { Banner, Field, Form, ModalChrome, TextField } from '@fuckyouryan/fui'

  let {
    editingPool = null,
    onComplete,
    closeModal,
  }: {
    editingPool?: ProxyPool | null
    onComplete: () => void | Promise<void>
    closeModal: () => void
  } = $props()

  // Initial values only: the modal owns this component's lifetime.
  // svelte-ignore state_referenced_locally
  let name = $state(editingPool?.name ?? '')
  // svelte-ignore state_referenced_locally
  let entriesText = $state(
    (editingPool?.entries ?? []).map((e: { url: string; country?: string }) =>
      e.country ? `${e.url} ${e.country}` : e.url
    ).join('\n')
  )
  let saving = $state(false)
  let error = $state('')

  function parseEntries(text: string): Array<{ url: string; country?: string }> {
    return text
      .split('\n')
      .map((line) => line.trim())
      .filter((line) => line !== '')
      .map((line) => {
        const [url, country] = line.split(/\s+/, 2)
        return country ? { url, country } : { url }
      })
  }

  async function save(): Promise<void> {
    if (!name.trim() || saving) return
    saving = true
    error = ''
    try {
      // Edit passes the existing id so renames keep the same pool;
      // create omits it and the backend slugifies the name.
      await api.proxies.pools.save({ id: editingPool?.id, name: name.trim(), entries: parseEntries(entriesText) })
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
  { label: saving ? t('common.actions.saving') : t('common.actions.save'), buttonStyle: 'prominent', onclick: save, disabled: saving || !name.trim() },
]} />

{#if error}
  <Banner tint="danger" text={error} />
{/if}
<Form>
  <Field label={t('common.labels.name')}>
    <TextField
      bind:value={name}
      placeholder={t('proxy.custom_pools.name')}
      onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') void save() }}
    />
  </Field>
  <Field label={t('proxy.title')}>
    <TextField maxRows={10} minRows={4} bind:value={entriesText} placeholder={t('proxy.custom_pools.hint')} />
  </Field>
</Form>
