<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import { t } from '../../lib/i18n.svelte'
  import type { ModalButton, Provider, UINode } from '../../lib/types'
  import DynamicForm, { collectButtons, buttonVariant } from '../domain/DynamicForm.svelte'
  import TextEdit from '../ui/controls/TextEdit.svelte'
  import TextArea from '../ui/controls/TextArea.svelte'
  import Select from '../ui/controls/Select.svelte'
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

  let types = $state<string[]>([])
  let typeKey = $state('custom')
  let name = $state('')
  let qualifier = $state('')
  let iconURL = $state('')
  let configNodes = $state<UINode[] | null>(null)
  let configValues = $state<Record<string, unknown>>({})
  let rawConfig = $state('{}')
  let useRawConfig = $state(false)
  let creating = $state(false)
  let error = $state('')

  const isEdit = $derived(!!editingProvider)

  onMount(async (): Promise<void> => {
    try {
      types = await api.providers.adapterTypes()
    } catch (e) {
      error = getErrorMessage(e)
    }
    if (editingProvider) {
      name = editingProvider.name ?? ''
      typeKey = editingProvider.type_key || editingProvider.type || 'custom'
      qualifier = editingProvider.qualifier ?? ''
      iconURL = editingProvider.icon_url ?? ''
      configValues = { ...(editingProvider.config ?? {}) }
      if (typeKey === 'custom' && editingProvider.base_url) {
        configValues = { ...configValues, base_url: editingProvider.base_url }
      }
    }
    await loadConfigSchema()
    syncButtons()
  })

  async function loadConfigSchema(): Promise<void> {
    configNodes = null
    useRawConfig = false
    if (!typeKey || typeKey === 'virtual') return
    try {
      const schema = await api.providers.configSchemaForType(typeKey)
      if (schema.nodes) {
        configNodes = schema.nodes
      } else {
        useRawConfig = true
        rawConfig = JSON.stringify(configValues, null, 2)
      }
    } catch (e) {
      error = getErrorMessage(e)
      useRawConfig = true
    }
  }

  async function onTypeChange(next: string): Promise<void> {
    typeKey = next
    configValues = {}
    await loadConfigSchema()
    syncButtons()
  }

  function syncButtons(): void {
    const tree = collectButtons(configNodes ?? [])
    // Config trees carry no server-side steps: any tree button saves the
    // whole form, its action is display-only.
    const buttons: ModalButton[] = tree.map((n) => ({
      label: n.text || (isEdit ? t('Save') : t('Add')),
      variant: buttonVariant(n),
      onClick: save,
      disabled: !name.trim() || creating,
      loading: creating,
    }))
    if (!tree.some((n) => (n.form_action || 'submit') === 'cancel')) {
      buttons.push({ label: t('Cancel'), variant: 'secondary', onClick: closeModal })
    }
    if (tree.length === 0) {
      buttons.push({
        label: isEdit ? t('Save') : t('Add'),
        variant: 'primary',
        onClick: save,
        disabled: !name.trim() || creating,
        loading: creating,
      })
    }
    updateButtons(buttons)
  }

  function collectConfig(): Record<string, unknown> | null {
    if (useRawConfig) {
      try {
        return JSON.parse(rawConfig) as Record<string, unknown>
      } catch {
        error = t('Config is not valid JSON')
        return null
      }
    }
    return { ...configValues }
  }

  async function save(): Promise<void> {
    if (!name.trim()) return
    creating = true
    error = ''
    syncButtons()
    try {
      const config = collectConfig()
      if (config === null) {
        creating = false
        syncButtons()
        return
      }
      if (editingProvider) {
        await api.providers.updateInstance(editingProvider.id, {
          name: name.trim(),
          config,
          icon_url: iconURL.trim()
        })
      } else {
        await api.providers.createInstance({
          name: name.trim(),
          type_key: typeKey,
          qualifier: qualifier.trim() || undefined,
          config,
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

  {#if !isEdit}
    <VStack gap={1}>
      <Text size="sm" weight="medium">{t('Type')} *</Text>
      <Select
        value={typeKey}
        options={types.map((t) => ({ value: t, label: t }))}
        onchange={onTypeChange}
      />
    </VStack>
  {/if}

  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('Name')} *</Text>
    <TextEdit
      id="provider-name"
      bind:value={name}
      hint={t('My LLM Provider')}
      onchange={syncButtons}
    />
  </VStack>

  {#if !isEdit && typeKey !== 'custom' && typeKey !== 'virtual'}
    <VStack gap={1}>
      <Text size="sm" weight="medium">{t('Qualifier')} ({t('optional')})</Text>
      <TextEdit
        id="provider-qualifier"
        bind:value={qualifier}
        hint="eu"
      />
      <Text size="sm" tone="soft">{t('Distinguishes multiple providers of the same type')}</Text>
    </VStack>
  {/if}

  {#if configNodes}
    <DynamicForm nodes={configNodes} bind:values={configValues} busy={creating} />
  {:else if useRawConfig}
    <VStack gap={1}>
      <Text size="sm" weight="medium">{t('Config JSON')}</Text>
      <TextArea id="provider-config" minRows={5} bind:value={rawConfig} />
    </VStack>
  {/if}

  <VStack gap={1}>
    <Text size="sm" weight="medium">{t('Icon URL')} ({t('optional')})</Text>
    <TextEdit
      id="icon-url"
      bind:value={iconURL}
      hint="https://example.com/icon.svg"
    />
  </VStack>
</VStack>
