<script lang="ts">
  import { squircle } from '../../../lib/squircle'
  import { t } from '../../../lib/i18n.svelte'
  import { getErrorMessage } from '../../../lib/errors'
  import { toast } from '../../../lib/toast.svelte'
  import { modal } from '../../../lib/modal.svelte'
  import { CAPABILITY_META } from '../../../lib/capabilities'
  import Table from '../../../components/ui/composite/Table.svelte'
  import ModelsTable from '../../../components/ui/composite/ModelsTable.svelte'
  import type { ModelsTableModel } from '../../../components/ui/composite/ModelsTable.svelte'
  import CopyButton from '../../../components/ui/composite/CopyButton.svelte'
  import Button from '../../../components/ui/controls/Button.svelte'
  import Spacer from '../../../components/ui/layout/Spacer.svelte'
  import HStack from '../../../components/ui/layout/HStack.svelte'
  import VStack from '../../../components/ui/layout/VStack.svelte'
  import Text from '../../../components/ui/controls/Text.svelte'
  import Switch from '../../../components/ui/controls/Switch.svelte'
  import Select from '../../../components/ui/controls/Select.svelte'
  import FloatingList from '../../../components/ui/controls/FloatingList.svelte'
  import Picker from '../../../components/ui/controls/Picker.svelte'
  import SearchField from '../../../components/ui/controls/SearchField.svelte'
  import TextEdit from '../../../components/ui/controls/TextEdit.svelte'
  import type { Provider, ProviderModel, ProviderVMGroup, TestResult, VirtualModel } from '../../../lib/types'
  import { api } from '../../../lib/api'

  let { provider = $bindable(), onrefresh } = $props<{ provider: Provider | null; onrefresh?: () => void }>()
  const providerId = $derived(provider?.id ?? '')

  let error = $state('')

    const KNOWN_CAPABILITIES = ['tools', 'vision', 'audio', 'json_mode', 'structured_outputs', 'reasoning']

  $effect(() => {
    disableFailedModels = provider?.config?.disable_failed_models === true
    autoSyncModels = provider?.config?.models_auto_sync === true
  })

  $effect(() => {
    if (provider) {
      void reloadModels()
      void loadVmGroups()
    }
  })

  let models = $state<ProviderModel[]>([])

  let modelsLoading = $state(true)

  let modelsError = $state('')

  let modelSearch = $state('')
  let importing = $state(false)
  let modelFilter = $state<'all' | 'enabled' | 'disabled'>(loadFilter())

  let toolbarWidth = $state(1200)

  let searchOpen = $state(false)

  let toolbarStage = $derived<'full' | 'compact'>(
    toolbarWidth < 640 ? 'compact' : 'full'
  )

  function measure(node: HTMLElement) {
    const ro = new ResizeObserver((entries) => {
      toolbarWidth = entries[0].contentRect.width
    })
    ro.observe(node)
    return { destroy: () => ro.disconnect() }
  }

  let overflowActions = $derived.by(() => {
    return (['all', 'enabled', 'disabled'] as const).map((v) => ({
      id: 'filter:' + v,
      label: `${t('Show')}: ` + t(v[0].toUpperCase() + v.slice(1)),
      icon: modelFilter === v ? 'check' : undefined,
    }))
  })

  function handleOverflowAction(id: string): void {
    if (id.startsWith('filter:')) void saveModelsFilter(id.slice(7) as typeof modelFilter)
  }

  let overflowOpen = $state(false)
  let overflowAnchor = $state<HTMLElement>()

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
    const q = modelSearch.trim().toLowerCase()
    if (q) {
      list = list.filter((m) =>
        m.name.toLowerCase().includes(q) || m.display_name.toLowerCase().includes(q)
      )
    }
    return list
  })

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

  const VM_ENDPOINTS: Record<string, { path: string; hint: string }> = {
    chat: { path: '/v1/chat/completions', hint: 'Text chat' },
    transcription: { path: '/v1/audio/transcriptions', hint: 'Speech-to-text' },
    speech: { path: '/v1/audio/speech', hint: 'Text-to-speech' },
    images: { path: '/v1/images/generations', hint: 'Image generation' },
    embeddings: { path: '/v1/embeddings', hint: 'Text embeddings' },
  }

  async function reloadModels(): Promise<void> {
    // First load spins; later reloads patch in place so the page
    // does not flash and scroll does not jump.
    modelsLoading = true
    modelsError = ''
    try {
      models = await api.models.forProvider(providerId)
    } catch (e) {
      modelsError = getErrorMessage(e)
      models = []
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
    toast.error(`${m.name} failed: ${res.summary ? t(res.summary) : t('probe failed')}`)
  }

  async function probeModel(m: ProviderModel): Promise<TestResult> {
    modelTestResults = { ...modelTestResults, [m.name]: 'loading' }
    let res: TestResult
    try {
      const endpoint = probeEndpoint(m)
      res = await api.models.test(`${providerId}/${m.name}`, endpoint)
      if (res.ok) {
        // Store verified modalities on the model record (full id key).
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
      // Temporary quota is not death: never disable over it.
      if (disableFailedModels && !res.quota_exceeded) {
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
          // Temporary quota is not death: never disable over it.
          if (!res.quota_exceeded) failed.push(m)
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

  async function deleteCustomModel(m: ProviderModel): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('Delete custom model'),
      message: `${t('Remove')} "${m.name}" ${t('from this provider?')}`,
      severity: 'medium',
      confirmText: t('Delete'),
      confirmRole: 'destructive',
    })
    if (!confirmed) return
    try {
      await api.models.deleteOverride(providerId, m.name)
      await reloadModels()
    } catch (e) {
      modelsError = getErrorMessage(e)
    }
  }

  function testIcon(res: TestResult | 'loading' | undefined, idleTitle: string): { icon: string; title: string } {
    if (!res) return { icon: 'network_check', title: idleTitle }
    if (res === 'loading') return { icon: 'progress_activity', title: t('Testing…') }
    if (res.ok) return { icon: 'check_circle', title: `OK · ${res.latency_ms}ms` }
    return { icon: 'error', title: res.error ?? t('Failed') }
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

  <VStack tag="section" gap={4} class="provider-section">
    <Text tag="h2" size="md" weight="medium">{t('Available models')}</Text>

      <HStack align="center" gap={3}>
        {#if toolbarStage !== 'compact'}
          <SearchField bind:value={modelSearch} placeholder={t('Filter models…')} />
        {:else if searchOpen}
          <HStack align="center" gap={1} grow>
            <SearchField bind:value={modelSearch} placeholder={t('Filter models…')} />
            <Button size="small" icon={{ name: 'close' }} ariaLabel={t('Close search')} onclick={() => { searchOpen = false }} />
          </HStack>
        {:else}
          <Button size="small" icon={{ name: 'search' }} ariaLabel={t('Open search')} title={t('Filter models')} onclick={() => { searchOpen = true }} />
        {/if}
        {#if toolbarStage !== 'compact'}
          <Picker
            bind:value={modelFilter}
            options={[
              { value: 'all', label: t('All') },
              { value: 'enabled', label: t('Enabled') },
              { value: 'disabled', label: t('Disabled') },
            ]}
            ariaLabel={t('Model visibility filter')}
            onchange={(v) => void saveModelsFilter(v as typeof modelFilter)}
          />
        {/if}
      </HStack>

    <HStack align="center" gap={2} wrap class="models-subbar">
      <Button icon={{ name: importing ? 'sync' : 'download' }} onclick={importModels} disabled={importing}>
        {importing ? t('Importing…') : t('Import from /models')}
      </Button>
      <Button icon={{ name: testingAll ? 'stop' : 'network_check' }} onclick={testAllModels}>
        {testingAll ? t('Testing… click to cancel') : t('Test all')}
      </Button>
      {#if toolbarStage === 'compact'}
        <Button
          size="small"
          icon={{ name: 'more_vert' }}
          ariaLabel={t('More actions')}
          title={t('More actions')}
          onclick={(e) => {
            overflowAnchor = e.currentTarget as HTMLElement
            overflowOpen = !overflowOpen
          }}
        />
        <FloatingList
          bind:open={overflowOpen}
          anchor={overflowAnchor}
          label={t('More actions')}
          actions={overflowActions}
          onaction={handleOverflowAction}
        />
      {/if}
    </HStack>

    <HStack gap={4} wrap class="models-toggles">
      <Switch
        checked={disableFailedModels}
        label={t('Disable failing models')}
        onchange={(v) => { disableFailedModels = v; saveAutomation() }}
      />
      <Switch
        checked={autoSyncModels}
        label={t('Auto-sync models')}
        onchange={(v) => { autoSyncModels = v; saveAutomation() }}
      />
    </HStack>

    {#if modelsError}
      <VStack class="error-msg" gap={0}>
        <Text tone="danger" size="sm">{modelsError}</Text>
      </VStack>
    {:else}
      {@render modelsTable()}
    {/if}
  </VStack>

  <VStack tag="section" gap={4} class="provider-section">
    <VStack gap={1}>
      <Text tag="h2" size="md" weight="medium">{t('Virtual models')}</Text>
      <Text tone="soft" size="sm">{t('One managed fall-through model per served endpoint. Members follow enabled models and refresh on import.')}</Text>
    </VStack>

    {#if vmGroups.length === 0}
      <VStack align="center" gap={2} class="table-empty">
        <Text tone="soft" size="sm">{t('No endpoint groups on this provider yet.')}</Text>
      </VStack>
    {:else}
      <Table
        columns={[
          { key: 'vm', title: t('Virtual model'), width: '1.4fr' },
          { key: 'endpoint', title: t('Endpoint'), width: '1.2fr' },
          { key: 'count', title: t('Models'), width: '0.4fr' },
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
                  <CopyButton size="small" text={`virtual/${v.id}`} title={t('Copy model id')} ariaLabel={t('Copy model id')} />
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
                ariaLabel={t('Enable virtual model')}
                onchange={(en) => toggleVirtualModel(v, en)}
              />
            {/if}
          {/if}
        {/snippet}
        {#snippet empty()}
          <VStack align="center" gap={2} class="table-empty">
            <Text tone="soft" size="sm">{t('No endpoint groups on this provider yet.')}</Text>
          </VStack>
        {/snippet}
      </Table>
    {/if}
  </VStack>

  <VStack tag="section" gap={4} class="provider-section">
    <VStack gap={1}>
      <Text tag="h2" size="md" weight="medium">{t('Add custom model')}</Text>
      <Text tone="soft" size="sm">{t('For models the provider does not list in /models.')}</Text>
    </VStack>

    <VStack gap={3}>
      <HStack gap={2} align="center" wrap>
        <TextEdit
          bind:value={customId}
          hint={t('Model id (e.g. my-model-v1)')}
          class="custom-model-input"
        />
        <TextEdit
          bind:value={customName}
          hint={t('Display name')}
          class="custom-model-input"
        />
        <Spacer/>
        <Button
          icon={{ name: probingCustom ? 'progress_activity' : 'fact_check' }}
          disabled={!customId.trim() || probingCustom}
          onclick={probeCustomCapabilities}
        >
          {t('Check')}
        </Button>
        <Button
          style="prominent"
          icon={{ name: 'add' }}
          disabled={!customId.trim() || addingCustom}
          onclick={addCustomModel}
        >
          {t('Add model')}
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
            style={customCaps[cap] ? 'prominent' : 'none'}
            icon={meta?.icon ? { name: meta.icon } : undefined}
            title={meta ? t(meta.hint) : cap}
            ariaLabel={meta ? t(meta.label) : cap}
            onclick={() => { customCaps = { ...customCaps, [cap]: !customCaps[cap] } }}
          >{meta ? t(meta.label) : cap}</Button>
        {/each}
      </HStack>
    </VStack>
  </VStack>

{#snippet modelActions(m: ProviderModel)}
  {@const ti = testIcon(modelTestResults[m.name], t('Test model'))}
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
        tint="#dc2626"
        icon={{ name: 'delete' }}
        title={t('Delete custom model')}
        ariaLabel={t('Delete custom model')}
        onclick={() => deleteCustomModel(m)}
      />
    {/if}
    <Switch
      checked={!m.disabled}
      ariaLabel={t('Enable model')}
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
      <Text size="sm" tone="soft">{models.length === 0 ? t('No models reported by this provider.') : t('No models match the filter.')}</Text>
    {/snippet}
  </ModelsTable>
{/snippet}
