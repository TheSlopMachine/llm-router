<script lang="ts">
  import ModelsTable from '../../../components/domain/ModelsTable.svelte'
  import { squircle } from '../../../FUI/core/squircle'
  import { t, tb } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { toast } from '../../../FUI/core/toast.svelte'
  import { CAPABILITY_META, hasModalities } from '$lib/capabilities'
  import { filterByFields } from '$lib/filter'
  import type { Provider, ProviderModel, ProviderVMGroup, TestResult, VirtualModel } from '$lib/types'
  import { api } from '$lib/api'
  import { FloatingView, Table, CopyButton, Button, Spacer, HStack, VStack, Text, Switch, Select, Picker, SearchField, Toolbar, ToolbarItem, TextEdit, Banner, ConfirmAction } from '$ui'
  import EmptyState from '../../../FUI/composite/EmptyState.svelte'
  import type { ModelsTableModel } from '../../../components/domain/ModelsTable.svelte'

  let { provider = $bindable(), onrefresh, credRevision = 0 } = $props<{
    provider: Provider | null
    onrefresh?: () => void
    credRevision?: number
  }>()
  const providerId = $derived(provider?.id ?? '')

  let error = $state('')

    const KNOWN_CAPABILITIES = ['tools', 'vision', 'audio', 'json_mode', 'structured_outputs', 'reasoning']

  $effect(() => {
    disableFailedModels = provider?.config?.disable_failed_models === true
    autoSyncModels = provider?.config?.models_auto_sync === true
  })

  $effect(() => {
    // Parent-notification effect: every dependency listed explicitly.
    // credRevision bumps only on credential add, so a key added to a
    // credential-less provider retries discovery without clicks.
    void credRevision
    if (provider) {
      void reloadModels()
      void loadVmGroups()
    }
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

  let modelTestResults = $state<Record<string, TestResult | 'loading'>>({})

  let testingAll = $state(false)

  let disableFailedModels = $state(false)

  let autoSyncModels = $state(false)

  const resultTimers = new Map<string, ReturnType<typeof setTimeout>>()

  function flashTimer(key: string, clear: () => void): void {
    clearTimeout(resultTimers.get(key))
    resultTimers.set(key, setTimeout(clear, 3000))
  }

  let customId = $state('')

  let customName = $state('')

  let customCaps = $state<Record<string, boolean>>({})

  let addingCustom = $state(false)

  let probingCustom = $state(false)

  let customProbeNote = $state('')

  let filteredModels = $derived.by(() => {
    let list = models
    if (modelFilter === 'enabled') list = list.filter((m) => !m.disabled)
    if (modelFilter === 'disabled') list = list.filter((m) => m.disabled)
    return filterByFields(list, modelSearch, (m) => [m.name, m.display_name])
  })

  function needsModalityBackfill(res: TestResult, m: ProviderModel): boolean {
    return res.ok && !hasModalities(m)
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
      const cfg = await api.config.get()
      await api.config.update({ ...cfg, models_filter: v })
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
      modelsError = getErrorMessage(e)
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
      modelsError = getErrorMessage(e)
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
    return 'chat'
  }

  const VERIFIED_MODALITIES: Record<string, { in: string[]; out: string[] }> = {
    chat: { in: ['text'], out: ['text'] },
    vision: { in: ['text', 'image'], out: ['text'] },
    speech: { in: ['text'], out: ['audio'] },
    transcription: { in: ['audio'], out: ['text'] },
    image: { in: ['text'], out: ['image'] },
    embeddings: { in: ['text'], out: ['embedding'] },
  }

  function union(a: string[] | undefined, b: string[]): string[] {
    return [...new Set([...(a ?? []), ...b])]
  }

  function probeToast(m: ProviderModel, res: TestResult): void {
    if (res.ok) {
      toast.success(`${m.name} works · ${res.latency_ms}ms`)
      return
    }
    toast.error(`${m.name} failed: ${res.summary ? tb(res.summary) : t('credentials.probe_failed')}`)
  }

  function isFailingModel(res: TestResult): boolean {
    return res.code === 'model_unavailable' || res.code === 'not_found'
  }

  async function probeModel(m: ProviderModel): Promise<TestResult> {
    modelTestResults = { ...modelTestResults, [m.name]: 'loading' }
    let res: TestResult
    try {
      const endpoint = probeEndpoint(m)
      res = await api.models.test(`${providerId}/${m.name}`, endpoint)
      if (needsModalityBackfill(res, m)) {
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
      res = { ok: false, latency_ms: 0, error: getErrorMessage(e) }
    }
    modelTestResults = { ...modelTestResults, [m.name]: res }
    return res
  }

  async function testModel(m: ProviderModel): Promise<TestResult> {
    const res = await probeModel(m)
    probeToast(m, res)
    if (!res.ok) {
      // Disable only on model failures. Credential, provider, and transport
      // errors never disable a model.
      if (disableFailedModels && isFailingModel(res)) {
        await api.models.setOverride(providerId, m.name, { disabled: true })
        await reloadModels()
      }
    }
    flashTimer('model:' + m.name, () => {
      const next = { ...modelTestResults }
      delete next[m.name]
      modelTestResults = next
    })
    return res
  }

  let testAllCancel = $state(false)

  async function testAllModels(): Promise<void> {
    if (testingAll) {
      testAllCancel = true
      return
    }
    testingAll = true
    testAllCancel = false
    for (const key of [...resultTimers.keys()]) {
      if (key.startsWith('model:')) {
        clearTimeout(resultTimers.get(key))
        resultTimers.delete(key)
      }
    }
    try {
      const failed: ProviderModel[] = []
      for (const m of models) {
        if (testAllCancel) break
        if (m.disabled) continue
        const res = await probeModel(m)
        probeToast(m, res)
        if (!res.ok) {
          // Disable only on model failures. Credential, provider, and transport
          // errors never disable a model.
          if (isFailingModel(res)) failed.push(m)
        }
      }
      if (!testAllCancel && disableFailedModels) {
        for (const m of failed) {
          await api.models.setOverride(providerId, m.name, { disabled: true })
        }
        if (failed.length > 0) {
          toast.success(`${failed.length} failing model${failed.length === 1 ? '' : 's'} disabled`)
          await reloadModels()
        }
      }
    } finally {
      testingAll = false
      testAllCancel = false
    }
  }

  async function probeCustomCapabilities(): Promise<void> {
    const id = customId.trim()
    if (!id) return
    probingCustom = true
    customProbeNote = ''
    try {
      const caps = await api.models.probeCapabilities(`${providerId}/${id}`)
      const next: Record<string, boolean> = {}
      for (const c of caps) next[c] = true
      customCaps = next
      if (caps.length === 0) customProbeNote = 'No new capabilities detected'
    } catch (e) {
      customProbeNote = getErrorMessage(e)
    } finally {
      probingCustom = false
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
      customProbeNote = ''
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

  function testIcon(res: TestResult | 'loading' | undefined, idleTitle: string): { icon: string; title: string } {
    if (!res) return { icon: 'network_check', title: idleTitle }
    if (res === 'loading') return { icon: 'progress_activity', title: t('credentials.testing') }
    if (res.ok) return { icon: 'check_circle', title: `OK · ${res.latency_ms}ms` }
    return { icon: 'error', title: res.error ?? t('common.state.failed') }
  }

  async function saveAutomation(): Promise<void> {
    if (!provider) return
    error = ''
    try {
      // Patch only the two automation fields on top of whatever config the
      // server currently has -- Proxy settings live in ProxySection now, so
      // this must not try to reconstruct or clobber the proxy portion.
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
</script>

  {#if discoveryFailed && !modelsLoading}
    <Banner variant="error" text={modelsError} />
  {:else}
  <VStack tag="section" gap={4} class="provider-section">
    <Text variant="section-title">{t('models.list.available')}</Text>

    <Toolbar overflow="wrap" gap={3}>
      <ToolbarItem pinned fill="md">
        <SearchField bind:value={modelSearch} placeholder={t('models.filter.placeholder')} />
      </ToolbarItem>
      <Picker
        bind:value={modelFilter}
        options={[
          { value: 'all', label: t('common.actions.all') },
          { value: 'enabled', label: t('common.labels.enabled') },
          { value: 'disabled', label: t('common.labels.disabled') },
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
          onclick={testAllModels}
        />
      </ToolbarItem>
    </Toolbar>

    <VStack gap={3}>
      <HStack align="center" justify="between" gap={3}>
        <Text grow>{t('providers.detail.disable_failing')}</Text>
        <Switch
          checked={disableFailedModels}
          ariaLabel={t('providers.detail.disable_failing')}
          onchange={(v) => { disableFailedModels = v; saveAutomation() }}
        />
      </HStack>
      <HStack align="center" justify="between" gap={3}>
        <Text grow>{t('providers.detail.auto_sync')}</Text>
        <Switch
          checked={autoSyncModels}
          ariaLabel={t('providers.detail.auto_sync')}
          onchange={(v) => { autoSyncModels = v; saveAutomation() }}
        />
      </HStack>
    </VStack>

    {#if modelsError}
      <Banner variant="error" text={modelsError} />
    {:else}
      {@render modelsTable()}
    {/if}
  </VStack>

  <VStack tag="section" gap={4} class="provider-section">
    <VStack gap={1}>
      <Text variant="section-title">{t('virtual.models_plural')}</Text>
      <Text tone="soft" size="sm">{t('providers.models.fallthrough_description')}</Text>
    </VStack>

    {#if vmGroups.length === 0}
      <EmptyState title={t('providers.models.no_endpoint_groups')} />
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
              <VStack gap={1} align="start">
                <Text size="base" weight="medium">{v.name}</Text>
                <HStack gap={2} align="center">
                  <Text mono size="sm" class="id-text">virtual/{v.id}</Text>
                  <CopyButton size="small" text={`virtual/${v.id}`} title={t('models.actions.copy_id')} ariaLabel={t('models.actions.copy_id')} />
                </HStack>
              </VStack>
            {:else}
              <Text tone="disabled">—</Text>
            {/if}
          {:else if column.key === 'endpoint'}
            {@const ep = VM_ENDPOINTS[g.endpoint] ?? { path: g.endpoint, hint: g.label }}
            <Text size="sm" tone="soft" title={t(ep.hint)}>{ep.path}</Text>
          {:else if column.key === 'count'}
            <Text size="sm" tone="soft">{g.models.length}</Text>
          {:else}
            {#if g.virtual}
              {@const v = g.virtual}
              <Switch
                checked={!v.disabled}
                ariaLabel={t('virtual.enable')}
                onchange={(en) => toggleVirtualModel(v, en)}
              />
            {/if}
          {/if}
        {/snippet}
        {#snippet card({ row })}
          {@const g = row as ProviderVMGroup}
          <HStack gap={3} align="center">
            <VStack gap={1} grow>
              {#if g.virtual}
                {@const v = g.virtual}
                <Text variant="value">{v.name}</Text>
                <Text variant="caption" mono>virtual/{v.id}</Text>
              {:else}
                <Text tone="disabled">—</Text>
              {/if}
            </VStack>
            {#if g.virtual}
              {@const v = g.virtual}
              <span
                role="presentation"
                onclick={(e) => e.stopPropagation()}
                onkeydown={(e) => e.stopPropagation()}
              >
                <Switch
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
          <EmptyState title={t('providers.models.no_endpoint_groups')} />
        {/snippet}
      </Table>
    {/if}
  </VStack>

  <VStack tag="section" gap={4} class="provider-section">
    <VStack gap={1}>
      <Text variant="section-title">{t('models.actions.add_custom')}</Text>
      <Text tone="soft" size="sm">{t('models.custom.not_listed')}</Text>
    </VStack>

    <VStack gap={3}>
      <HStack gap={2} align="center" wrap>
        <TextEdit
          bind:value={customId}
          hint={t('models.custom.id_hint')}
          class="custom-model-input"
        />
        <TextEdit
          bind:value={customName}
          hint={t('models.custom.display_name')}
          class="custom-model-input"
        />
        <Spacer/>
        <Button
          icon={{ name: probingCustom ? 'progress_activity' : 'fact_check' }}
          disabled={!customId.trim() || probingCustom}
          onclick={probeCustomCapabilities}
        >
          {t('common.actions.check')}
        </Button>
        <Button
          style="prominent"
          icon={{ name: 'add' }}
          disabled={!customId.trim() || addingCustom}
          onclick={addCustomModel}
        >
          {t('models.actions.add')}
        </Button>
      </HStack>

      {#if customProbeNote}
        <Text tone="soft" size="sm">{customProbeNote}</Text>
      {/if}

      <HStack gap={2} wrap align="center">
        {#each KNOWN_CAPABILITIES as cap}
          {@const meta = CAPABILITY_META[cap]}
          <Button
            size="small"
            selected={!!customCaps[cap]}
            tint={meta?.tint}
            icon={meta?.icon ? { name: meta.icon } : undefined}
            title={meta ? t(meta.hint) : cap}
            ariaLabel={meta ? t(meta.label) : cap}
            onclick={() => { customCaps = { ...customCaps, [cap]: !customCaps[cap] } }}
          >{meta ? t(meta.label) : cap}</Button>
        {/each}
      </HStack>
    </VStack>
  </VStack>
  {/if}

{#snippet modelActions(m: ProviderModel)}
  {@const ti = testIcon(modelTestResults[m.name], t('models.actions.test'))}
  <HStack gap={2} class="model-actions">
    <Button
      size="small"
      icon={{ name: ti.icon }}
      title={ti.title}
      ariaLabel={ti.title}
      disabled={modelTestResults[m.name] === 'loading'}
      onclick={() => testModel(m)}
    />
    {#if m.custom}
      <Button
        size="small"
        tint="var(--fui-color-danger)"
        icon={{ name: 'delete' }}
        title={t('models.actions.delete_custom')}
        ariaLabel={t('models.actions.delete_custom')}
        onclick={(e) => openDeleteCustomModel(m, e.currentTarget as HTMLElement)}
      />
    {/if}
    <Switch
      checked={!m.disabled}
      ariaLabel={t('models.actions.enable')}
      onchange={(v) => toggleModel(m, v)}
    />
  </HStack>
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
    draggable
    loading={modelsLoading && models.length === 0}
    onReorder={(from, to) => {
      const visible = filteredModels
      const moved = visible[from]
      const target = visible[to]
      if (!moved || !target || moved.name === target.name) return
      const fromIndex = models.findIndex((m) => m.name === moved.name)
      const toIndex = models.findIndex((m) => m.name === target.name)
      if (fromIndex < 0 || toIndex < 0) return
      const next = [...models]
      const [item] = next.splice(fromIndex, 1)
      next.splice(toIndex, 0, item)
      models = next
    }}
  >
    {#snippet actions({ model })}
      {@const m = model.source as ProviderModel}
      {@render modelActions(m)}
    {/snippet}
    {#snippet empty()}
      <EmptyState title={models.length === 0 ? t('providers.models.empty') : t('providers.models.no_filter_match')} icon="search" />
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
      onCancel={close}
      onConfirm={confirmDeleteCustomModel} />
  {/snippet}
</FloatingView>

<style>
</style>
