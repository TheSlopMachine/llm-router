<script lang="ts">
  // Body of the New/Edit custom pool modal. Rendered by the shared Modal
  // shell (title, close button, footer chrome).
  import { untrack } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import type { ModalButton, ProxyPool } from '$lib/types'
  import { Text, TextArea, TextEdit, VStack, Banner } from '$ui'

  let {
    editingPool = null,
    onComplete,
    closeModal,
    updateButtons,
  } = $props<{
    editingPool?: ProxyPool | null
    onComplete: () => void | Promise<void>
    closeModal: () => void
    updateButtons: (buttons: ModalButton[]) => void
  }>()

  const isEditMode = $derived(editingPool !== null)

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
      const entries = parseEntries(entriesText)
      // Edit passes the existing id so renames keep the same pool;
      // create omits it and the backend slugifies the name.
      if (isEditMode && editingPool) {
        await api.proxies.pools.save({ id: editingPool.id, name: name.trim(), entries })
      } else {
        await api.proxies.pools.save({ name: name.trim(), entries })
      }
      await onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      saving = false
    }
  }

  $effect(() => {
    void saving
    void name
    untrack(() => {
      updateButtons([
        { label: t('common.actions.cancel'), variant: 'secondary', onClick: closeModal, disabled: saving },
        {
          label: saving ? t('common.actions.saving') : t('common.actions.save'),
          variant: 'primary',
          onClick: () => void save(),
          disabled: saving || !name.trim(),
        },
      ])
    })
  })
</script>

<VStack gap={4}>
  {#if error}
    <Banner variant="error" text={error} />
  {/if}
  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('common.labels.name')}</Text>
    <TextEdit
      bind:value={name}
      hint={t('proxy.custom_pools.name')}
      onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') void save() }}
    />
  </VStack>
  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('proxy.title')}</Text>
    <TextArea
      bind:value={entriesText}
      hint={t('proxy.custom_pools.hint')}
    />
  </VStack>
</VStack>
