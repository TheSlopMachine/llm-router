<script lang="ts">
  import { untrack } from 'svelte'
  import ModelsTable from '../../../components/domain/ModelsTable.svelte'
  import { squircle, toast, FloatingView, Table, CopyButton, Button, Spacer, HStack, VStack, Text, Toggle, Checkbox, Picker, SearchField, Toolbar, ToolbarItem, TextField, Banner, ConfirmAction, ContentUnavailable, type IconName } from '@fuckyouryan/fui'
  import { RelativeTime, Chip } from '@fuckyouryan/fui'
  import { modalityIcon, modalityTint } from '$lib/modalities'
  import type { TranslationKey } from '$lib/i18n.svelte'
  import { t, tb, tx, locale } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { CAPABILITY_META, hasModalities } from '$lib/capabilities'
  import { filterByFields } from '$lib/filter'
  import type { Provider, ProviderModel, ProviderVMGroup, TestResult, VirtualModel } from '$lib/types'
  import { api } from '$lib/api'
  import type { ModelsTableModel } from '../../../components/domain/ModelsTable.svelte'

  let { provider = $bindable(), onrefresh, credRevision = 0 }: {
    provider: Provider | null
    onrefresh?: () => void
    credRevision?: number
  } = $props()
  const providerId = $derived(provider?.id ?? '')

  let error = $state('')

    const KNOWN_CAPABILITIES = ['tools', 'vision', 'audio', 'json_mode', 'structured_outputs', 'reasoning']

  $effect(() => {
    // Dependencies: provider identity and credRevision only. credRevision bumps on
    // credential add, so a key added to a credential-less provider retries discovery.
    // Replacing the provider object (config toggles) must not reload the model list.
    void credRevision
    if (!providerId) return
    untrack(() => {
      loadChecks()
      void reloadModels()
    })
  })

  let models = $state<ProviderModel[]>([])

  let modelsLoading = $state(true)

  let modelsError = $state('')

  // Discovery failure is tracked separately from action errors: only a
  // failed catalog load hides the model sections. Failed toggles, tests
  // and imports write modelsError alone and leave everything visible.
  let discoveryFailed = $state(false)

  let modelSearch = $state('')
  let importing = $state(false)
  let modelFilter = $state<'all' | 'enabled' | 'disabled'>(loadFilter())

  // Last check per model, kept in this browser so the status column survives
  // reloads: the result and when it came.
  type CheckRecord = { result: TestResult; at: string }
  let modelTestResults = $state<Record<string, TestResult | 'loading'>>({})
  let lastChecks = $state<Record<string, CheckRecord>>({})

  function checksKey(): string {
    return `llm-router:model-checks:${providerId}`
  }

  function loadChecks(): void {
    try {
      const raw = localStorage.getItem(checksKey())
      lastChecks = raw ? (JSON.parse(raw) as Record<string, CheckRecord>) : {}
    } catch {
      lastChecks = {}
    }
  }

  function rememberCheck(name: string, result: TestResult): void {
    lastChecks = { ...lastChecks, [name]: { result, at: new Date().toISOString() } }
    try {
      localStorage.setItem(checksKey(), JSON.stringify(lastChecks))
    } catch {
      // storage full or blocked: the status still shows for this session
    }
  }

  let testingAll = $state(false)

  // Writable deriveds: the toggle writes override locally until the provider
  // prop changes, then the saved config wins again.
  let disableFailedModels = $derived(provider?.config?.disable_failed_models === true)
  let autoSyncModels = $derived(provider?.config?.models_auto_sync === true)

  async function saveAutomation(): Promise<void> {
    if (!provider) return
    error = ''
    try {
      // Patch only the two automation fields on top of the saved config.
      await api.providers.updateInstance(provider.id, {
        name: provider.name,
        config: {
          ...(provider.config ?? {}),
          disable_failed_models: disableFailedModels,
          models_auto_sync: autoSyncModels,
        },
      })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? provider
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  // Filters: modalities (keys "mod:<name>", input or output; text is
  // everywhere, so it is not offered) and capabilities. Only what some model
  // has is offered; a model must match every picked filter.
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

  let customId = $state('')

  let customName = $state('')

  let customCaps = $state<Record<string, boolean>>({})

  let addingCustom = $state(false)

  let filteredModels = $derived.by(() => {
    let list = models
    if (modelFilter === 'enabled') list = list.filter((m) => !m.disabled)
    if (modelFilter === 'disabled') list = list.filter((m) => m.disabled)
    if (capFilter.length > 0) list = list.filter((m) => capFilter.every((key) => hasFeature(m, key)))
    return filterByFields(list, modelSearch, (m) => [m.name, m.display_name])
  })

  function needsModalityBackfill(result: TestResult, m: ProviderModel): boolean {
    return result.ok && !hasModalities(m)
  }

  function filterKey(): string {
    return `llm-router:model-filter:${providerId}`
  }

  function loadFilter(): 'all' | 'enabled' | 'disabled' {
    const v = localStorage.getItem(filterKey())
    return v === 'all' || v === 'disabled' ? v : 'enabled'
  }

  async function saveModelsFilter(v: typeof modelFilter): Promise<void> {
    modelFilter = v
    localStorage.setItem(filterKey(), v)
    try {
      const routerConfig = await api.config.get()
      await api.config.update({ ...routerConfig, models_filter: v })
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  let vmGroups = $state<ProviderVMGroup[]>([])
  // Managed records are ensured once per provider view: the toggle needs a
  // record, and sync is idempotent and cache-only.
  let vmSyncEnsuredFor = $state('')

  async function loadVmGroups(): Promise<void> {
    try {
      const groups = await api.providers.virtualModels(providerId)
      if (groups.some((g) => !g.virtual) && vmSyncEnsuredFor !== providerId) {
        vmSyncEnsuredFor = providerId
        vmGroups = await api.providers.syncVirtualModels(providerId).catch(() => groups)
      } else {
        vmGroups = groups
      }
    } catch {
      vmGroups = []
    }
  }

  async function toggleVirtualModel(vm: VirtualModel, enabled: boolean): Promise<void> {
    try {
      await api.virtualModels.update(vm.id, { ...vm, disabled: !enabled })
      await loadVmGroups()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  const VM_ENDPOINTS: Record<string, { path: string; hint: import('$lib/i18n.svelte').TranslationKey }> = {
    chat: { path: '/v1/chat/completions', hint: 'models.endpoints.text_chat' },
    transcription: { path: '/v1/audio/transcriptions', hint: 'models.endpoints.speech_to_text' },
    speech: { path: '/v1/audio/speech', hint: 'models.endpoints.text_to_speech' },
    images: { path: '/v1/images/generations', hint: 'models.endpoints.image_gen' },
    embeddings: { path: '/v1/embeddings', hint: 'models.endpoints.text_embeddings' },
  }

  async function reloadModels(): Promise<void> {
    // First load spins; later reloads patch in place so the page
    // does not flash and scroll does not jump.
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
    void loadVmGroups()
  }

  async function importModels(): Promise<void> {
    if (importing) return
    importing = true
    modelsError = ''
    try {
      await api.models.refresh(providerId)
      await reloadModels()
      await loadVmGroups()
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
    const eps = m.endpoints ?? []
    if (eps.length === 0) return 'chat'
    if (eps.includes('chat/completions')) {
      if ((m.input_modalities ?? []).includes('image')) return 'vision'
      return 'chat'
    }
    if (eps.includes('audio/speech')) return 'speech'
    if (eps.includes('embeddings')) return 'embeddings'
    if (eps.includes('images/generations')) return 'image'
    if (eps.includes('audio/transcriptions')) return 'transcription'
    if (eps.includes('videos')) return 'video'
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
      if (needsModalityBackfill(result, m)) {
        // Store verified modalities on the model record (full id key).
        // Rows with plugin-declared modalities skip this: the probe only
        // proves liveness, and a redundant write would mask model_specs.
        // Best-effort: a failed write must not fail the probe itself.
        try {
          const v = VERIFIED_MODALITIES[endpoint] ?? VERIFIED_MODALITIES.chat
          await api.models.setOverride(providerId, m.name, {
            input_modalities: union(m.input_modalities, v.in),
            output_modalities: union(m.output_modalities, v.out),
          })
          m.input_modalities = union(m.input_modalities, v.in)
          m.output_modalities = union(m.output_modalities, v.out)
        } catch {
          // Ignore: liveness is proven, metadata sync is cosmetic.
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

  async function testModel(m: ProviderModel): Promise<TestResult> {
    const result = await probeModel(m)
    let disabled = false
    if (!result.ok) {
      // Disable only on model failures. Credential, provider, and transport
      // errors never disable a model.
      if (disableFailedModels && isFailingModel(result)) {
        await api.models.setOverride(providerId, m.name, { disabled: true })
        disabled = true
        await reloadModels()
      }
    }
    if (result.ok) {
      toast.success(tx('models.test.ok', { name: m.name, ms: result.latency_ms }))
    } else {
      let text = tx('models.test.failed', { name: m.name, reason: failureReason(result) })
      if (disabled) text += ' ' + tx('models.test.disabled', { count: 1 })
      toast.error(text)
    }
    return result
  }

  let testAllCancel = $state(false)

  // Selection: a checkbox per row; picking any row brings up the bulk action bar.
  let selected = $state<string[]>([])
  let bulkBusy = $state(false)
  const visibleNames = $derived(filteredModels.map((m) => m.name))
  const allVisibleSelected = $derived(visibleNames.length > 0 && visibleNames.every((n) => selected.includes(n)))

  // The bar opens with the first pick and closes only by its close button.
  let bulkOpen = $state(false)

  function setSelected(name: string, on: boolean): void {
    selected = on ? [...new Set([...selected, name])] : selected.filter((n) => n !== name)
    if (on) bulkOpen = true
  }

  function toggleSelectAll(): void {
    selected = allVisibleSelected
      ? selected.filter((n) => !visibleNames.includes(n))
      : [...new Set([...selected, ...visibleNames])]
  }

  function clearSelection(): void {
    selected = []
    bulkOpen = false
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
          // Test all skips priced media generations: each video job bills on
          // render, each image bills per image. Single model tests probe them.
          const endpoint = probeEndpoint(m)
          if (endpoint === 'video' || endpoint === 'image') continue
        }
        const result = await probeModel(m)
        tested++
        if (!result.ok) {
          failedCount++
          // Disable only on model failures. Credential, provider, and transport
          // errors never disable a model.
          if (isFailingModel(result)) failed.push(m)
        }
      }
      let disabledCount = 0
      if (!testAllCancel && disableFailedModels) {
        for (const m of failed) {
          await api.models.setOverride(providerId, m.name, { disabled: true })
        }
        disabledCount = failed.length
        if (failed.length > 0) await reloadModels()
      }
      reportCheck(tested, failedCount, disabledCount)
    } finally {
      testingAll = false
      testAllCancel = false
    }
  }

  async function addCustomModel(): Promise<void> {
    const id = customId.trim()
    if (!id) return
    addingCustom = true
    try {
      const caps = KNOWN_CAPABILITIES.filter((c) => customCaps[c])
      await api.models.setOverride(providerId, id, {
        custom: true,
        display_name: customName.trim(),
        capabilities: caps,
      })
      customId = ''
      customName = ''
      customCaps = {}
      await reloadModels()
    } catch (e) {
      modelsError = getErrorMessage(e)
    } finally {
      addingCustom = false
    }
  }

  let deleteCustomTarget = $state<ProviderModel | null>(null)
  let deleteCustomAnchor = $state<HTMLElement>()
  let deletingCustom = $state(false)

  function openDeleteCustomModel(m: ProviderModel, anchorEl?: HTMLElement): void {
    if (deleteCustomTarget?.name === m.name) {
      deleteCustomTarget = null
    } else {
      deleteCustomTarget = m
      deleteCustomAnchor = anchorEl
    }
  }

  async function confirmDeleteCustomModel(): Promise<void> {
    const m = deleteCustomTarget
    if (!m || deletingCustom) return
    deletingCustom = true
    try {
      await api.models.deleteOverride(providerId, m.name)
      deleteCustomTarget = null
      await reloadModels()
    } catch (e) {
      modelsError = getErrorMessage(e)
    } finally {
      deletingCustom = false
    }
  }

  function testIcon(result: TestResult | 'loading' | undefined, idleTitle: string): { icon: IconName; title: string } {
    if (!result) return { icon: 'network_check', title: idleTitle }
    if (result === 'loading') return { icon: 'progress_activity', title: t('credentials.testing') }
    if (result.ok) return { icon: 'check_circle', title: `OK · ${result.latency_ms}ms` }
    return { icon: 'error', title: result.error ?? t('common.state.failed') }
  }

</script>

  {#if discoveryFailed && !modelsLoading}
    <Banner tint="danger" text={modelsError} />
  {:else}
  <VStack tag="section" gap={4} class="provider-section">
    <Text variant="section-title">{t('models.list.available')}</Text>

    <Toolbar overflow="wrap" gap={3}>
      <Picker pickerStyle="segmented"
        bind:value={modelFilter}
        options={[
          { value: 'all', label: `${t('common.actions.all')} ${filterCounts.all}` },
          { value: 'enabled', label: `${t('common.labels.enabled')} ${filterCounts.enabled}` },
          { value: 'disabled', label: `${t('common.labels.disabled')} ${filterCounts.disabled}` },
        ]}
        ariaLabel={t('models.filter.visibility')}
        onchange={(v) => void saveModelsFilter(v as typeof modelFilter)}
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

    <div class="setting-grid">
      <Text>{t('providers.detail.disable_failing')}</Text>
      <Toggle
        checked={disableFailedModels}
        ariaLabel={t('providers.detail.disable_failing')}
        onchange={(v) => { disableFailedModels = v; void saveAutomation() }}
      />
      <Text>{t('providers.detail.auto_sync')}</Text>
      <Toggle
        checked={autoSyncModels}
        ariaLabel={t('providers.detail.auto_sync')}
        onchange={(v) => { autoSyncModels = v; void saveAutomation() }}
      />
    </div>

    {#if modalityOptions.length > 0 || capabilityOptions.length > 0}
      <!-- filters: a page block like the other sections; every chip wears its
           modality/capability colour, the unpicked ones muted -->
      <VStack gap={4} class="filter-section">
        <Text variant="section-title">{t('models.filter.label')}</Text>
        <div class="filter-grid">
          {#if modalityOptions.length > 0}
            <Text size="sm" foregroundStyle="secondary">{t('models.filter.modalities')}</Text>
            <HStack gap={3} wrap valign="center">
              {#each modalityOptions as mod (mod)}
                <Chip
                  icon={modalityIcon(mod)}
                  text={t(`modality.${mod}` as TranslationKey)}
                  tint={modalityTint(mod)}
                  class={capFilter.includes('mod:' + mod) ? '' : 'chip-off'}
                  onclick={() => toggleFeature('mod:' + mod)}
                />
              {/each}
            </HStack>
          {/if}
          {#if capabilityOptions.length > 0}
            <Text size="sm" foregroundStyle="secondary">{t('models.filter.capabilities')}</Text>
            <HStack gap={3} wrap valign="center">
              {#each capabilityOptions as cap (cap)}
                {@const meta = CAPABILITY_META[cap]}
                <Chip
                  icon={meta.icon}
                  text={t(meta.label)}
                  title={t(meta.hint)}
                  tint={meta.chipTint}
                  class={capFilter.includes(cap) ? '' : 'chip-off'}
                  onclick={() => toggleFeature(cap)}
                />
              {/each}
            </HStack>
          {/if}
        </div>
      </VStack>
    {/if}

    <!-- search sits right over the table it narrows -->
    <SearchField bind:value={modelSearch} placeholder={t('models.filter.placeholder')} />

    {#if modelsError}
      <Banner tint="danger" text={modelsError} />
    {:else}
      {@render modelsTable()}
      {#if bulkOpen}
        <!-- bulk actions over the picked rows; stays in view while the list
             scrolls and until its close button, whatever the selection holds -->
        <div class="bulk-bar">
          <HStack gap={2} wrap valign="center" pad={3} radius="lg" elev>
            <span class="bulk-count"><Text size="sm" weight="medium">{t('models.bulk.count')}: {selected.length}</Text></span>
            <Spacer />
            <Button
              size="sm"
              icon={{ name: allVisibleSelected ? 'deselect' : 'select_all' }}
              text={allVisibleSelected ? t('models.bulk.select_none') : t('common.actions.select_all')}
              disabled={visibleNames.length === 0}
              onclick={toggleSelectAll}
            />
            <Button size="sm" icon={{ name: 'toggle_on' }} text={t('models.bulk.enable')} disabled={bulkBusy || selected.length === 0} onclick={() => void setSelectedDisabled(false)} />
            <Button size="sm" icon={{ name: 'toggle_off' }} text={t('models.bulk.disable')} disabled={bulkBusy || selected.length === 0} onclick={() => void setSelectedDisabled(true)} />
            <Button
              size="sm"
              icon={{ name: testingAll ? 'stop' : 'network_check' }}
              text={testingAll ? t('credentials.testing_click_cancel') : t('models.bulk.test')}
              disabled={!testingAll && selected.length === 0}
              onclick={testingAll ? () => void testAllModels() : testSelected}
            />
            <Button size="sm" buttonStyle="text" icon={{ name: 'close' }} title={t('models.bulk.clear')} ariaLabel={t('models.bulk.clear')} onclick={clearSelection} />
          </HStack>
        </div>
      {/if}
    {/if}
  </VStack>

  <VStack tag="section" gap={4} class="provider-section">
    <VStack gap={1}>
      <Text variant="section-title">{t('virtual.models_plural')}</Text>
      <Text foregroundStyle="secondary" size="sm">{t('providers.models.fallthrough_description')}</Text>
    </VStack>

    {#if vmGroups.length === 0}
      <ContentUnavailable title={t('providers.models.no_endpoint_groups')} />
    {:else}
      <Table
        columns={[
          { key: 'vm', title: t('virtual.model'), width: '1.4fr' },
          { key: 'endpoint', title: t('providers.models.endpoint'), width: '1.2fr' },
          { key: 'count', title: t('models.list.title'), width: '0.4fr' },
          { key: 'toggle', width: 'auto' },
        ]}
        rows={vmGroups}
        rowKey={(g) => g.endpoint}
      >
        {#snippet cell({ column, row })}
          {@const g = row as ProviderVMGroup}
          {#if column.key === 'vm'}
            {#if g.virtual}
              {@const v = g.virtual}
              <VStack gap={1} halign="start">
                <Text size="md" weight="medium">{v.name}</Text>
                <HStack gap={2} valign="center">
                  <Text mono size="sm" class="id-text">virtual/{v.id}</Text>
                  <CopyButton size="sm" text={`virtual/${v.id}`} title={t('models.actions.copy_id')} ariaLabel={t('models.actions.copy_id')} />
                </HStack>
              </VStack>
            {:else}
              <Text foregroundStyle="tertiary">—</Text>
            {/if}
          {:else if column.key === 'endpoint'}
            {@const ep = VM_ENDPOINTS[g.endpoint] ?? { path: g.endpoint, hint: g.label }}
            <Text size="sm" foregroundStyle="secondary" title={t(ep.hint)}>{ep.path}</Text>
          {:else if column.key === 'count'}
            <Text size="sm" foregroundStyle="secondary">{g.models.length}</Text>
          {:else}
            {#if g.virtual}
              {@const v = g.virtual}
              <Toggle
                checked={!v.disabled}
                ariaLabel={t('virtual.enable')}
                onchange={(en) => toggleVirtualModel(v, en)}
              />
            {/if}
          {/if}
        {/snippet}
        {#snippet card({ row })}
          {@const g = row as ProviderVMGroup}
          <HStack gap={3} valign="center">
            <VStack gap={1} grow>
              {#if g.virtual}
                {@const v = g.virtual}
                <Text variant="value">{v.name}</Text>
                <Text variant="caption" mono>virtual/{v.id}</Text>
              {:else}
                <Text foregroundStyle="tertiary">—</Text>
              {/if}
            </VStack>
            {#if g.virtual}
              {@const v = g.virtual}
              <span
                role="presentation"
                onclick={(e) => e.stopPropagation()}
                onkeydown={(e) => e.stopPropagation()}
              >
                <Toggle
                  checked={!v.disabled}
                  ariaLabel={t('virtual.enable')}
                  onchange={(en) => toggleVirtualModel(v, en)}
                />
              </span>
            {/if}
          </HStack>
          {@const ep = VM_ENDPOINTS[g.endpoint] ?? { path: g.endpoint, hint: g.label }}
          <Text variant="caption">{ep.path} · {g.models.length} {t('models.list.title')}</Text>
        {/snippet}
        {#snippet empty()}
          <ContentUnavailable title={t('providers.models.no_endpoint_groups')} />
        {/snippet}
      </Table>
    {/if}
  </VStack>

  <VStack tag="section" gap={4} class="provider-section">
    <VStack gap={1}>
      <Text variant="section-title">{t('models.actions.add_custom')}</Text>
      <Text foregroundStyle="secondary" size="sm">{t('models.custom.not_listed')}</Text>
    </VStack>

    <VStack gap={3}>
      <HStack gap={2} valign="center" wrap>
        <TextField
          bind:value={customId}
          placeholder={t('models.custom.id_hint')}
          class="custom-model-input"
        />
        <TextField
          bind:value={customName}
          placeholder={t('models.custom.display_name')}
          class="custom-model-input"
        />
        <Spacer/>
        <Button
          buttonStyle="prominent"
          icon={{ name: 'add' }}
          disabled={!customId.trim() || addingCustom}
          onclick={addCustomModel}
        >
          {t('models.actions.add')}
        </Button>
      </HStack>

      <HStack gap={2} wrap valign="center">
        {#each KNOWN_CAPABILITIES as cap}
          {@const capability = CAPABILITY_META[cap]}
          <Button
            size="sm"
            selected={!!customCaps[cap]}
            tint={capability?.tint}
            icon={capability?.icon ? { name: capability.icon } : undefined}
            title={capability ? t(capability.hint) : cap}
            ariaLabel={capability ? t(capability.label) : cap}
            onclick={() => { customCaps = { ...customCaps, [cap]: !customCaps[cap] } }}
          >{capability ? t(capability.label) : cap}</Button>
        {/each}
      </HStack>
    </VStack>
  </VStack>
  {/if}

{#snippet modelActions(m: ProviderModel)}
  {@const ti = testIcon(modelTestResults[m.name], t('models.actions.test'))}
  <HStack gap={2} class="model-actions">
    <Button
      size="sm"
      icon={{ name: ti.icon }}
      title={ti.title}
      ariaLabel={ti.title}
      disabled={modelTestResults[m.name] === 'loading'}
      onclick={() => testModel(m)}
    />
    {#if m.custom}
      <Button
        size="sm"
        icon={{ name: 'delete' }}
        title={t('models.actions.delete_custom')}
        ariaLabel={t('models.actions.delete_custom')}
        onclick={(e) => openDeleteCustomModel(m, e.currentTarget as HTMLElement)}
      />
    {/if}
    <Toggle
      checked={!m.disabled}
      ariaLabel={t('models.actions.enable')}
      onchange={(v) => toggleModel(m, v)}
    />
  </HStack>
{/snippet}

{#snippet modelStatus(m: ProviderModel)}
  {@const check = lastChecks[m.name]}
  {#if modelTestResults[m.name] === 'loading'}
    <Text size="sm" foregroundStyle="secondary">{t('credentials.testing')}</Text>
  {:else if check}
    <VStack gap={0} halign="start">
      {#if check.result.ok}
        <Text size="sm" tint="success">✓ {check.result.latency_ms} {t('models.status.ms')}</Text>
      {:else}
        <Text size="sm" tint="danger" title={check.result.error ?? failureReason(check.result)}>✗ {failureReason(check.result)}</Text>
      {/if}
      <Text size="xs" foregroundStyle="tertiary"><RelativeTime date={check.at} locale={locale()} /></Text>
    </VStack>
  {:else}
    <Text size="sm" foregroundStyle="tertiary">—</Text>
  {/if}
{/snippet}

{#snippet modelsTable()}
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
    loading={modelsLoading && models.length === 0}
    statusValue={(model) => {
      const check = lastChecks[(model.source as ProviderModel).name]
      if (!check) return 2
      return check.result.ok ? 0 : 1
    }}
  >
    {#snippet select({ model })}
      {@const m = model.source as ProviderModel}
      <span class="select-cell">
        <Checkbox
          checked={selected.includes(m.name)}
          ariaLabel={t('models.bulk.select_row')}
          onchange={(v) => setSelected(m.name, v)}
        />
      </span>
    {/snippet}
    {#snippet status({ model })}
      {@render modelStatus(model.source as ProviderModel)}
    {/snippet}
    {#snippet actions({ model })}
      {@const m = model.source as ProviderModel}
      {@render modelActions(m)}
    {/snippet}
    {#snippet empty()}
      {#if models.length === 0}
        <ContentUnavailable title={t('providers.models.empty')} />
      {:else}
        <ContentUnavailable title={t('providers.models.no_filter_match')} icon="search" />
      {/if}
    {/snippet}
  </ModelsTable>
{/snippet}

<FloatingView
  open={Boolean(deleteCustomTarget)}
  anchor={deleteCustomAnchor}
  width="sm"
  onclose={() => { deleteCustomTarget = null }}
  label={t('models.actions.delete_custom')}
>
  {#snippet children({ close })}
    <ConfirmAction
      title={t('models.actions.delete_custom')}
      body={`${t('common.actions.remove')} "${deleteCustomTarget?.name}" ${t('providers.models.delete_from')}`}
      busy={deletingCustom}
      busyLabel={t('common.actions.deleting')}
      oncancel={close}
      onconfirm={confirmDeleteCustomModel} />
  {/snippet}
</FloatingView>

<style>
  /* label | switch pairs: switches sit in one column right after the longest label */
  .setting-grid {
    display: grid;
    grid-template-columns: auto auto;
    justify-content: start;
    align-items: center;
    gap: var(--fui-space-3) var(--fui-space-5);
  }
  /* the count reads as a label among small buttons: inset like their text */
  .bulk-count {
    display: inline-flex;
    padding-inline: var(--fui-btn-pad-h-sm);
  }
  /* label | chip row pairs: the chip rows start in one column; rows are as
     far apart as the chips in a row (space-3) */
  .filter-grid {
    display: grid;
    grid-template-columns: auto 1fr;
    align-items: center;
    gap: var(--fui-space-3) var(--fui-space-4);
  }
  /* The filters sit inside the models section (gap space-4) but read as a
     section of their own: the page puts space-6 between sections. */
  :global(.filter-section) {
    margin-top: calc(var(--fui-space-6) - var(--fui-space-4));
  }
  /* an unpicked filter chip keeps its colour, lightly muted */
  :global(.chip-off) {
    opacity: 0.75;
  }
  /* The selection column is the table's first: its left inset is the table's
     edge padding (space-5), its right is this cell's plus the next cell's
     (2 x space-2). The margin evens the two sides out. */
  .select-cell {
    display: inline-flex;
    margin-right: calc(var(--fui-space-5) - 2 * var(--fui-space-2));
  }
  /* bulk actions stick to the bottom of the scroll area while rows are picked */
  .bulk-bar {
    position: sticky;
    bottom: var(--fui-space-4);
    z-index: 2;
    filter: var(--fui-filter-floating);
  }
</style>
