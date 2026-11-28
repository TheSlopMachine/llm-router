<script lang="ts">
  // The provider's models: discovery, filters, checks, bulk actions, custom models, and the
  // provider's virtual models below them.
  import { onMount } from 'svelte'
  import ModelsTable from '../../../components/domain/ModelsTable.svelte'
  import ProviderVirtualModels from './ProviderVirtualModels.svelte'
  import AddCustomModelModal from './AddCustomModelModal.svelte'
  import { toast, Button, DeleteButton, Grid, Label, Section, SelectionBar, HStack, VStack, Text, Toggle, Checkbox, Picker, SearchField, Toolbar, ToolbarItem, Banner, ContentUnavailable, RelativeTime, Chip, confirmAt, type IconName } from '@fuckyouryan/fui'
  import { modalityIcon, modalityTint } from '$lib/modalities'
  import { t, tb, tx, type TranslationKey } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { CAPABILITY_META, hasModalities } from '$lib/capabilities'
  import { filterByFields } from '$lib/filter'
  import { openFormModal } from '$lib/modal-helpers'
  import { Persisted, oneOf } from '$lib/persisted.svelte'
  import type { Provider, ProviderModel, TestResult } from '$lib/types'
  import { api } from '$lib/api'
  import type { ModelsTableModel } from '../../../components/domain/ModelsTable.svelte'

  let { provider = $bindable(), waitingText = '' }: {
    provider: Provider
    /** set while the provider's containers prepare: a failed discovery shows it instead of the error */
    waitingText?: string
  } = $props()
  const providerId = $derived(provider.id)

  let error = $state('')
  let models = $state.raw<ProviderModel[]>([])
  let modelsLoading = $state(true)
  let modelsError = $state('')
  // Discovery failure is tracked separately from action errors: only a failed catalog load hides
  // the model sections. Failed toggles, tests and imports write modelsError alone.
  let discoveryFailed = $state(false)
  let modelSearch = $state('')
  let importing = $state(false)
  let virtualModels = $state<ProviderVirtualModels>()

  type ModelFilter = 'all' | 'enabled' | 'disabled'
  // Last check per model, kept in this browser so the status column survives reloads.
  type CheckRecord = { result: TestResult; at: string }
  // The page mounts once per provider; the stores follow the provider id all the same.
  const filterChoice = $derived(new Persisted<ModelFilter>(`llm-router:model-filter:${providerId}`, 'enabled', { parse: oneOf(['all', 'enabled', 'disabled'] as const) }))
  const checks = $derived(new Persisted<Record<string, CheckRecord>>(`llm-router:model-checks:${providerId}`, {}, {
    parse: (stored) => JSON.parse(stored) as Record<string, CheckRecord>,
    serialize: (value) => JSON.stringify(value),
  }))
  let modelTestResults = $state<Record<string, TestResult | 'loading'>>({})

  onMount(() => void reloadModels())

  /** retries discovery: a key added to a credential-less provider, or containers that finished preparing */
  export function retryDiscovery(): void {
    void reloadModels()
  }

  function rememberCheck(name: string, result: TestResult): void {
    checks.value = { ...checks.value, [name]: { result, at: new Date().toISOString() } }
  }

  let testingAll = $state(false)

  // Writable deriveds: the toggle writes override locally until the provider prop changes, then
  // the saved config wins again.
  let disableFailedModels = $derived(provider.config?.disable_failed_models === true)
  let autoSyncModels = $derived(provider.config?.models_auto_sync === true)

  async function saveAutomation(): Promise<void> {
    error = ''
    try {
      // Patch only the two automation fields on top of the saved config.
      await api.providers.updateInstance(provider.id, {
        name: provider.name,
        config: { ...(provider.config ?? {}), disable_failed_models: disableFailedModels, models_auto_sync: autoSyncModels },
      })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? provider
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  // Filters: modalities (keys "mod:<name>", input or output; text is everywhere, so it is not
  // offered) and capabilities. Only what some model has is offered; a model must match every
  // picked filter.
  const MODALITY_ORDER = ['image', 'audio', 'video', 'file', 'speech', 'transcription', 'embedding']
  let capFilter = $state<string[]>([])

  function hasFeature(m: ProviderModel, key: string): boolean {
    if (key.startsWith('mod:')) {
      const modality = key.slice(4)
      return (m.input_modalities ?? []).includes(modality) || (m.output_modalities ?? []).includes(modality)
    }
    return (m.capabilities ?? []).includes(key)
  }

  const modalityOptions = $derived(MODALITY_ORDER.filter((mod) => models.some((m) => hasFeature(m, 'mod:' + mod))))
  const capabilityOptions = $derived(Object.keys(CAPABILITY_META).filter((cap) => models.some((m) => hasFeature(m, cap))))

  function toggleFeature(key: string): void {
    capFilter = capFilter.includes(key) ? capFilter.filter((k) => k !== key) : [...capFilter, key]
  }

  const filterCounts = $derived({
    all: models.length,
    enabled: models.filter((m) => !m.disabled).length,
    disabled: models.filter((m) => m.disabled).length,
  })

  let filteredModels = $derived.by(() => {
    let list = models
    if (filterChoice.value === 'enabled') list = list.filter((m) => !m.disabled)
    if (filterChoice.value === 'disabled') list = list.filter((m) => m.disabled)
    if (capFilter.length > 0) list = list.filter((m) => capFilter.every((key) => hasFeature(m, key)))
    return filterByFields(list, modelSearch, (m) => [m.name, m.display_name])
  })

  async function saveModelsFilter(next: ModelFilter): Promise<void> {
    filterChoice.value = next
    try {
      const routerConfig = await api.config.get()
      await api.config.update({ ...routerConfig, models_filter: next })
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function reloadModels(): Promise<void> {
    modelsLoading = true
    modelsError = ''
    discoveryFailed = false
    try {
      models = await api.models.forProvider(providerId)
    } catch (e) {
      modelsError = `${t('models.discovery_failed')}: ${getErrorMessage(e)}`
      models = []
      discoveryFailed = true
    } finally {
      modelsLoading = false
    }
    // Group composition follows the list above.
    void virtualModels?.reload()
  }

  async function importModels(): Promise<void> {
    if (importing) return
    importing = true
    modelsError = ''
    try {
      await api.models.refresh(providerId)
      await reloadModels()
    } catch (e) {
      // the previous list stays on screen; only the reason is added
      modelsError = `${t('models.discovery_failed')}: ${getErrorMessage(e)}`
    } finally {
      importing = false
    }
  }

  async function toggleModel(m: ProviderModel, enabled: boolean): Promise<void> {
    try {
      await api.models.setOverride(providerId, m.name, { disabled: !enabled })
      await reloadModels()
    } catch (e) {
      modelsError = getErrorMessage(e)
    }
  }

  function probeEndpoint(m: ProviderModel): string {
    const endpoints = m.endpoints ?? []
    if (endpoints.length === 0) return 'chat'
    if (endpoints.includes('chat/completions')) return (m.input_modalities ?? []).includes('image') ? 'vision' : 'chat'
    if (endpoints.includes('audio/speech')) return 'speech'
    if (endpoints.includes('embeddings')) return 'embeddings'
    if (endpoints.includes('images/generations')) return 'image'
    if (endpoints.includes('audio/transcriptions')) return 'transcription'
    if (endpoints.includes('videos')) return 'video'
    return 'chat'
  }

  const VERIFIED_MODALITIES: Record<string, { in: string[]; out: string[] }> = {
    chat: { in: ['text'], out: ['text'] },
    vision: { in: ['text', 'image'], out: ['text'] },
    speech: { in: ['text'], out: ['audio'] },
    transcription: { in: ['audio'], out: ['text'] },
    image: { in: ['text'], out: ['image'] },
    embeddings: { in: ['text'], out: ['embedding'] },
    video: { in: ['text'], out: ['video'] },
  }

  function union(a: string[] | undefined, b: string[]): string[] {
    return [...new Set([...(a ?? []), ...b])]
  }

  function failureReason(result: TestResult): string {
    return result.summary ? tb(result.summary) : t('credentials.probe_failed')
  }

  // One toast per check: a single model, the picked models or all of them.
  function reportCheck(tested: number, failed: number, disabled: number): void {
    if (tested === 0) return
    let text = failed === 0
      ? tx('models.test.batch_ok', { count: tested })
      : tx('models.test.batch_failed', { count: tested, ok: tested - failed, failed })
    if (disabled > 0) text += ' ' + tx('models.test.disabled', { count: disabled })
    if (failed === 0) toast.success(text)
    else toast.error(text)
  }

  function isFailingModel(result: TestResult): boolean {
    return result.code === 'model_unavailable' || result.code === 'not_found'
  }

  async function probeModel(m: ProviderModel): Promise<TestResult> {
    modelTestResults = { ...modelTestResults, [m.name]: 'loading' }
    let result: TestResult
    try {
      const endpoint = probeEndpoint(m)
      result = await api.models.test(`${providerId}/${m.name}`, endpoint)
      if (result.ok && !hasModalities(m)) {
        // Store verified modalities on the model record. Rows with plugin-declared modalities skip
        // this: the probe only proves liveness, and a redundant write would mask model_specs.
        const verified = VERIFIED_MODALITIES[endpoint] ?? VERIFIED_MODALITIES.chat
        const modalities = { input_modalities: union(m.input_modalities, verified.in), output_modalities: union(m.output_modalities, verified.out) }
        try {
          await api.models.setOverride(providerId, m.name, modalities)
          models = models.map((model) => (model.name === m.name ? { ...model, ...modalities } : model))
        } catch (e) {
          // the model works; only its modalities stay unrecorded
          modelsError = getErrorMessage(e)
        }
      }
    } catch (e) {
      result = { ok: false, latency_ms: 0, error: getErrorMessage(e) }
    }
    const next = { ...modelTestResults }
    delete next[m.name]
    modelTestResults = next
    rememberCheck(m.name, result)
    return result
  }

  async function testModel(m: ProviderModel): Promise<void> {
    const result = await probeModel(m)
    if (result.ok) {
      toast.success(tx('models.test.ok', { name: m.name, ms: result.latency_ms }))
      return
    }
    let text = tx('models.test.failed', { name: m.name, reason: failureReason(result) })
    // Disable only on model failures. Credential, provider, and transport errors never disable a model.
    if (disableFailedModels && isFailingModel(result)) {
      try {
        await api.models.setOverride(providerId, m.name, { disabled: true })
        text += ' ' + tx('models.test.disabled', { count: 1 })
        await reloadModels()
      } catch (e) {
        modelsError = getErrorMessage(e)
      }
    }
    toast.error(text)
  }

  let testAllCancel = $state(false)

  // Selection: a checkbox per row; picking any row brings up the bulk action bar.
  let selected = $state<string[]>([])
  let bulkBusy = $state(false)
  const visibleNames = $derived(filteredModels.map((m) => m.name))
  const allVisibleSelected = $derived(visibleNames.length > 0 && visibleNames.every((name) => selected.includes(name)))

  function setSelected(name: string, on: boolean): void {
    selected = on ? [...new Set([...selected, name])] : selected.filter((other) => other !== name)
  }

  function toggleSelectAll(): void {
    selected = allVisibleSelected
      ? selected.filter((name) => !visibleNames.includes(name))
      : [...new Set([...selected, ...visibleNames])]
  }

  async function setSelectedDisabled(disabled: boolean): Promise<void> {
    if (bulkBusy || selected.length === 0) return
    bulkBusy = true
    modelsError = ''
    try {
      for (const name of selected) await api.models.setOverride(providerId, name, { disabled })
      await reloadModels()
    } catch (e) {
      modelsError = getErrorMessage(e)
    } finally {
      bulkBusy = false
    }
  }

  function testSelected(): void {
    // An explicit pick: disabled and media models are probed too, like a single test.
    void testAllModels(models.filter((m) => selected.includes(m.name)))
  }

  async function testAllModels(only?: ProviderModel[]): Promise<void> {
    if (testingAll) {
      testAllCancel = true
      return
    }
    testingAll = true
    testAllCancel = false
    try {
      const failed: ProviderModel[] = []
      let tested = 0
      let failedCount = 0
      for (const m of only ?? models) {
        if (testAllCancel) break
        if (!only) {
          if (m.disabled) continue
          // Test all skips priced media generations: each video job bills on render, each image
          // bills per image. Single model tests probe them.
          const endpoint = probeEndpoint(m)
          if (endpoint === 'video' || endpoint === 'image') continue
        }
        const result = await probeModel(m)
        tested++
        if (!result.ok) {
          failedCount++
          if (isFailingModel(result)) failed.push(m)
        }
      }
      let disabledCount = 0
      if (!testAllCancel && disableFailedModels && failed.length > 0) {
        try {
          for (const m of failed) await api.models.setOverride(providerId, m.name, { disabled: true })
          disabledCount = failed.length
          await reloadModels()
        } catch (e) {
          modelsError = getErrorMessage(e)
        }
      }
      reportCheck(tested, failedCount, disabledCount)
    } finally {
      testingAll = false
      testAllCancel = false
    }
  }

  function openAddCustomModel(): void {
    openFormModal(AddCustomModelModal, { title: t('models.actions.add_custom'), size: 'md', props: { providerId }, onReload: () => void reloadModels() })
  }

  async function deleteCustomModel(m: ProviderModel, anchor: HTMLElement): Promise<void> {
    modelsError = ''
    try {
      const deleted = await confirmAt(anchor, {
        title: t('models.actions.delete_custom'),
        body: `${t('common.actions.remove')} "${m.name}" ${t('providers.models.delete_from')}`,
        busyLabel: t('common.actions.deleting'),
        run: () => api.models.deleteOverride(providerId, m.name),
      })
      if (deleted) await reloadModels()
    } catch (e) {
      modelsError = getErrorMessage(e)
    }
  }

  function testIcon(result: TestResult | 'loading' | undefined): { icon: IconName; title: string } {
    if (!result) return { icon: 'network_check', title: t('models.actions.test') }
    if (result === 'loading') return { icon: 'progress_activity', title: t('credentials.testing') }
    if (result.ok) return { icon: 'check_circle', title: `OK · ${result.latency_ms}ms` }
    return { icon: 'error', title: result.error ?? t('common.state.failed') }
  }
</script>

{#snippet modelActions(m: ProviderModel)}
  {@const icon = testIcon(modelTestResults[m.name])}
  <HStack>
    <Button icon={{ name: icon.icon }} title={icon.title} ariaLabel={icon.title} disabled={modelTestResults[m.name] === 'loading'} onclick={() => testModel(m)} />
    {#if m.custom}
      <DeleteButton label={t('models.actions.delete_custom')} onclick={(e) => void deleteCustomModel(m, e.currentTarget as HTMLElement)} />
    {/if}
    <Toggle checked={!m.disabled} ariaLabel={t('common.labels.enabled')} onchange={(next) => toggleModel(m, next)} />
  </HStack>
{/snippet}

{#snippet modelStatus(m: ProviderModel)}
  {@const check = checks.value[m.name]}
  {#if modelTestResults[m.name] === 'loading'}
    <Text size="sm" foregroundStyle="secondary">{t('credentials.testing')}</Text>
  {:else if check}
    <VStack spacing="tight">
      {#if check.result.ok}
        <Label tint="success" text={`${check.result.latency_ms} ${t('models.status.ms')}`} />
      {:else}
        <Label tint="danger" text={failureReason(check.result)} title={check.result.error ?? failureReason(check.result)} />
      {/if}
      <Text size="xs" foregroundStyle="tertiary"><RelativeTime date={check.at} /></Text>
    </VStack>
  {:else}
    <Text size="sm" foregroundStyle="tertiary">—</Text>
  {/if}
{/snippet}

{#if discoveryFailed && !modelsLoading && waitingText}
  <Banner tint="info" text={tx('models.discovery_waiting.status', { detail: waitingText })} />
{:else if discoveryFailed && !modelsLoading}
  <Banner tint="danger" text={modelsError} />
{:else}
  {#snippet addCustomModelAction()}
    <Button buttonStyle="prominent" icon={{ name: 'add' }} text={t('models.actions.add_custom')} onclick={openAddCustomModel} />
  {/snippet}
  <Section title={t('models.list.available')} actions={addCustomModelAction}>
    {#if error}
      <Banner tint="danger" text={error} />
    {/if}

    <Toolbar overflow="wrap">
      <Picker pickerStyle="segmented"
        value={filterChoice.value}
        options={[
          { value: 'all', label: t('common.actions.all'), badge: filterCounts.all },
          { value: 'enabled', label: t('common.labels.enabled'), badge: filterCounts.enabled },
          { value: 'disabled', label: t('common.labels.disabled'), badge: filterCounts.disabled },
        ]}
        ariaLabel={t('models.filter.visibility')}
        onchange={(next) => void saveModelsFilter(next as ModelFilter)}
      />
      <ToolbarItem priority={2}>
        <Button
          icon={{ name: importing ? 'sync' : 'download' }}
          text={importing ? t('providers.detail.importing') : t('providers.detail.import_models')}
          onclick={importModels}
          disabled={importing}
        />
      </ToolbarItem>
      <ToolbarItem priority={1}>
        <Button
          icon={{ name: testingAll ? 'stop' : 'network_check' }}
          text={testingAll ? t('credentials.testing_click_cancel') : t('credentials.test_all')}
          onclick={() => void testAllModels()}
        />
      </ToolbarItem>
    </Toolbar>

    <!-- label | switch pairs: the switches line up after the longest label -->
    <Grid cols="max-content max-content" valign="center">
      <Text>{t('providers.detail.disable_failing')}</Text>
      <Toggle checked={disableFailedModels} ariaLabel={t('providers.detail.disable_failing')} onchange={(next) => { disableFailedModels = next; void saveAutomation() }} />
      <Text>{t('providers.detail.auto_sync')}</Text>
      <Toggle checked={autoSyncModels} ariaLabel={t('providers.detail.auto_sync')} onchange={(next) => { autoSyncModels = next; void saveAutomation() }} />
    </Grid>
  </Section>

  {#if modalityOptions.length > 0 || capabilityOptions.length > 0}
    <!-- filters: every chip wears its modality or capability colour, the unpicked ones muted -->
    <Section title={t('models.filter.label')}>
      <Grid cols="max-content 1fr" valign="center">
        {#if modalityOptions.length > 0}
          <Text size="sm" foregroundStyle="secondary">{t('models.filter.modalities')}</Text>
          <HStack wrap valign="center">
            {#each modalityOptions as mod (mod)}
              <Chip
                icon={modalityIcon(mod)}
                text={t(`modality.${mod}` as TranslationKey)}
                tint={modalityTint(mod)}
                selected={capFilter.includes('mod:' + mod)}
                onclick={() => toggleFeature('mod:' + mod)}
              />
            {/each}
          </HStack>
        {/if}
        {#if capabilityOptions.length > 0}
          <Text size="sm" foregroundStyle="secondary">{t('models.filter.capabilities')}</Text>
          <HStack wrap valign="center">
            {#each capabilityOptions as cap (cap)}
              {@const meta = CAPABILITY_META[cap]}
              <Chip
                icon={meta.icon}
                text={t(meta.label)}
                title={t(meta.hint)}
                tint={meta.chipTint}
                selected={capFilter.includes(cap)}
                onclick={() => toggleFeature(cap)}
              />
            {/each}
          </HStack>
        {/if}
      </Grid>
    </Section>
  {/if}

  <!-- the search sits right over the table it narrows -->
  <Section>
    <SearchField bind:value={modelSearch} placeholder={t('models.filter.placeholder')} />

    {#if modelsError}
      <Banner tint="danger" text={modelsError} />
    {/if}
    <ModelsTable
      models={filteredModels.map((m): ModelsTableModel => ({
        kind: 'model',
        id: m.name,
        fullId: `${providerId}/${m.name}`,
        name: m.display_name || m.name,
        contextWindow: m.context_window,
        maxTokens: m.max_tokens,
        inputModalities: m.input_modalities,
        outputModalities: m.output_modalities,
        capabilities: m.capabilities,
        disabled: m.disabled,
        custom: m.custom,
        source: m,
      }))}
      sortable
      lazy
      maxHeight="70vh"
      loading={modelsLoading && models.length === 0}
      statusValue={(model) => {
        const check = checks.value[(model.source as ProviderModel).name]
        if (!check) return 2
        return check.result.ok ? 0 : 1
      }}
    >
      {#snippet select({ model })}
        {@const m = model.source as ProviderModel}
        <Checkbox checked={selected.includes(m.name)} ariaLabel={t('models.bulk.select_row')} onchange={(next) => setSelected(m.name, next)} />
      {/snippet}
      {#snippet status({ model })}
        {@render modelStatus(model.source as ProviderModel)}
      {/snippet}
      {#snippet actions({ model })}
        {@render modelActions(model.source as ProviderModel)}
      {/snippet}
      {#snippet empty()}
        {#if models.length === 0}
          <ContentUnavailable title={t('providers.models.empty')} />
        {:else}
          <ContentUnavailable title={t('providers.models.no_filter_match')} icon="search" />
        {/if}
      {/snippet}
    </ModelsTable>
    {#if selected.length > 0}
      <SelectionBar text={`${t('models.bulk.count')}: ${selected.length}`} onclear={() => { selected = [] }}>
        {#snippet actions()}
          <Button
            icon={{ name: allVisibleSelected ? 'deselect' : 'select_all' }}
            text={allVisibleSelected ? t('models.bulk.select_none') : t('common.actions.select_all')}
            disabled={visibleNames.length === 0}
            onclick={toggleSelectAll}
          />
          <Button icon={{ name: 'toggle_on' }} text={t('models.bulk.enable')} disabled={bulkBusy} onclick={() => void setSelectedDisabled(false)} />
          <Button icon={{ name: 'toggle_off' }} text={t('models.bulk.disable')} disabled={bulkBusy} onclick={() => void setSelectedDisabled(true)} />
          <Button
            icon={{ name: testingAll ? 'stop' : 'network_check' }}
            text={testingAll ? t('credentials.testing_click_cancel') : t('models.bulk.test')}
            onclick={testingAll ? () => void testAllModels() : testSelected}
          />
        {/snippet}
      </SelectionBar>
    {/if}
  </Section>

  <ProviderVirtualModels bind:this={virtualModels} {providerId} onerror={(message) => { error = message }} />
{/if}
