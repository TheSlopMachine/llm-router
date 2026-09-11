<script lang="ts">
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import { toast } from '../lib/toast.svelte'
  import type { Provider, Credential, ProviderModel, TestResult, Proxy } from '../lib/types'
  import CredentialWizard from '../components/CredentialWizard.svelte'
  import CredentialEditModal from '../components/CredentialEditModal.svelte'
  import CustomProviderWizard from '../components/wizards/CustomProviderWizard.svelte'
  import SegmentedControl from '../components/ui/SegmentedControl.svelte'
  import ActionDropdown from '../components/ActionDropdown.svelte'
  import Switch from '../components/ui/Switch.svelte'
  import { squircle } from '../lib/squircle'

  let { providerId } = $props<{ providerId: string }>()

  let provider = $state<Provider | null>(null)
  let credentials = $state<Credential[]>([])
  let models = $state<ProviderModel[]>([])
  let modelsError = $state('')
  let loading = $state(true)
  let error = $state('')

  let modelSearch = $state('')
  let modelFilter = $state<'all' | 'enabled' | 'disabled'>('all')

  // Toolbar overflow cascade: full → buttons into menu → search into icon.
  let toolbarWidth = $state(1200)
  let searchOpen = $state(false)
  let toolbarStage = $derived<'full' | 'menu' | 'compact'>(
    toolbarWidth < 700 ? 'compact' : toolbarWidth < 1050 ? 'menu' : 'full'
  )

  // Attaches width measurement when the node appears (toolbar renders
  // only after data loads, so onMount is too early).
  function measure(node: HTMLElement) {
    const ro = new ResizeObserver((entries) => {
      toolbarWidth = entries[0].contentRect.width
    })
    ro.observe(node)
    return { destroy: () => ro.disconnect() }
  }

  let overflowActions = $derived.by(() => {
    const core = [
      { id: 'import', label: 'Import from /models', icon: 'download' },
      { id: 'test_all', label: testingAll ? 'Testing…' : 'Test all', icon: 'network_check', disabled: testingAll },
    ]
    if (toolbarStage !== 'compact') return core
    const filters = (['all', 'enabled', 'disabled'] as const).map((v) => ({
      id: 'filter:' + v,
      label: 'Show: ' + v[0].toUpperCase() + v.slice(1),
      icon: modelFilter === v ? 'check' : undefined,
    }))
    return [...filters, ...core]
  })

  function handleOverflowAction(id: string): void {
    if (id === 'import') importModels()
    else if (id === 'test_all') testAllModels()
    else if (id.startsWith('filter:')) modelFilter = id.slice(7) as typeof modelFilter
  }

  // Provider proxy settings (stored in provider.config.proxy)
  let proxyMode = $state<'disabled' | 'auto' | 'manual'>('disabled')
  let proxyIds = $state<Record<string, boolean>>({})
  let poolManual = $state<Proxy[]>([])
  let savingProxy = $state(false)
  let proxySaved = $state(false)
  let proxySavedTimer: ReturnType<typeof setTimeout> | undefined

  let credentialTestResults = $state<Record<string, TestResult | 'loading'>>({})
  let modelTestResults = $state<Record<string, TestResult | 'loading'>>({})
  let testingAll = $state(false)

  const resultTimers = new Map<string, ReturnType<typeof setTimeout>>()

  // Shows a test result briefly, then clears it back to the idle icon.
  function flashTimer(key: string, clear: () => void): void {
    clearTimeout(resultTimers.get(key))
    resultTimers.set(key, setTimeout(clear, 3000))
  }

  let dragCredId = $state('')

  // Custom model form
  let customId = $state('')
  let customName = $state('')
  let customCaps = $state<Record<string, boolean>>({})
  let addingCustom = $state(false)
  let probingCustom = $state(false)
  let customProbeNote = $state('')

  const KNOWN_CAPABILITIES = ['tools', 'vision', 'audio', 'json_mode', 'structured_outputs', 'reasoning']

  const CAPABILITY_META: Record<string, { label: string; cls: string; icon: string; hint: string }> = {
    tools: { label: 'Tools', cls: 'chip-blue', icon: 'build', hint: 'Tool calling — the model can invoke functions' },
    vision: { label: 'Vision', cls: 'chip-purple', icon: 'image', hint: 'Vision — accepts image input' },
    audio: { label: 'Audio', cls: 'chip-teal', icon: 'graphic_eq', hint: 'Audio — accepts audio input' },
    json_mode: { label: 'JSON', cls: 'chip-yellow', icon: 'data_object', hint: 'JSON mode — response_format: json_object' },
    structured_outputs: { label: 'Structured', cls: 'chip-yellow', icon: 'schema', hint: 'Structured outputs — responses follow a JSON schema' },
    reasoning: { label: 'Reasoning', cls: 'chip-orange', icon: 'psychology', hint: 'Reasoning — thinks before answering (reasoning_content)' },
  }

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

  $effect(() => {
    void providerId
    loadPage()
  })

  async function loadPage(): Promise<void> {
    loading = true
    error = ''
    try {
      const [providers, creds] = await Promise.all([api.providers.list(), api.credentials.list()])
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? null
      credentials = sortCredentials((creds as Credential[]).filter((c) => c.provider_id === providerId))
      if (provider) {
        initProxyConfig()
        await reloadModels()
        poolManual = (await api.proxies.list()).filter((p) => p.source === 'manual')
      }
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  function sortCredentials(list: Credential[]): Credential[] {
    return [...list].sort((a, b) => {
      const oa = a.order ?? 0
      const ob = b.order ?? 0
      if ((oa > 0) !== (ob > 0)) return oa > 0 ? -1 : 1
      if (oa > 0 && ob > 0 && oa !== ob) return oa - ob
      return 0
    })
  }

  function initProxyConfig(): void {
    const raw = (provider?.config?.proxy ?? {}) as { mode?: string; ids?: string[] }
    proxyMode = raw.mode === 'auto' || raw.mode === 'manual' ? raw.mode : 'disabled'
    proxyIds = {}
    for (const id of raw.ids ?? []) proxyIds[id] = true
  }

  async function saveProxyConfig(): Promise<void> {
    if (!provider) return
    savingProxy = true
    error = ''
    try {
      const ids = Object.keys(proxyIds).filter((id) => proxyIds[id])
      await api.providers.updateInstance(provider.id, {
        name: provider.name,
        config: {
          ...provider.config,
          proxy: { mode: proxyMode, ...(proxyMode === 'manual' ? { ids } : {}) },
        },
      })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? provider
      proxySaved = true
      clearTimeout(proxySavedTimer)
      proxySavedTimer = setTimeout(() => { proxySaved = false }, 2000)
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      savingProxy = false
    }
  }

  async function reloadCredentials(): Promise<void> {
    const creds = await api.credentials.list()
    credentials = sortCredentials((creds as Credential[]).filter((c) => c.provider_id === providerId))
  }

  async function reloadModels(): Promise<void> {
    modelsError = ''
    try {
      models = await api.models.forProvider(providerId)
    } catch (e) {
      modelsError = getErrorMessage(e)
      models = []
    }
  }

  async function importModels(): Promise<void> {
    modelsError = ''
    try {
      await api.models.refresh(providerId)
      await reloadModels()
    } catch (e) {
      modelsError = getErrorMessage(e)
    }
  }

  function back(): void {
    window.location.hash = '#/providers'
  }

  function openAddCredential(): void {
    if (!provider) return
    modal.open({
      title: `Add Credential · ${provider.name}`,
      content: CredentialWizard,
      severity: 'medium',
      size: 'medium',
      props: {
        provider,
        onComplete: async () => {
          modal.close()
          await reloadCredentials()
        },
      },
    })
  }

  function openEditCredential(cred: Credential): void {
    modal.open({
      title: `Edit Key · ${cred.label || 'Unnamed'}`,
      content: CredentialEditModal,
      severity: 'medium',
      size: 'medium',
      props: {
        credential: cred,
        onComplete: async () => {
          modal.close()
          await reloadCredentials()
        },
      },
    })
  }

  function openEditProvider(): void {
    if (!provider) return
    modal.open({
      title: 'Edit Provider',
      content: CustomProviderWizard,
      severity: 'medium',
      size: 'medium',
      props: {
        editingProvider: provider,
        onComplete: async () => {
          modal.close()
          await loadPage()
        },
      },
    })
  }

  async function deleteProvider(): Promise<void> {
    if (!provider) return
    const confirmed = await modal.confirm({
      title: 'Delete Provider',
      message: `Are you sure you want to delete "${provider.name}"? Credentials for this provider will be removed as well.`,
      severity: 'high',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true,
    })
    if (!confirmed) return
    try {
      await api.providers.delete(provider.id)
      back()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function deleteCredential(cred: Credential): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Delete key',
      message: `Are you sure you want to delete "${cred.label || 'Unnamed'}"? This action cannot be undone.`,
      severity: 'medium',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true,
    })
    if (!confirmed) return
    try {
      await api.credentials.delete(cred.id)
      credentials = credentials.filter((c) => c.id !== cred.id)
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function toggleCredential(cred: Credential, enabled: boolean): Promise<void> {
    try {
      await api.credentials.update(cred.id, { disabled: !enabled })
      await reloadCredentials()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function testCredential(cred: Credential): Promise<void> {
    credentialTestResults = { ...credentialTestResults, [cred.id]: 'loading' }
    let res: TestResult
    try {
      res = await api.credentials.test(cred.id)
    } catch (e) {
      res = { ok: false, latency_ms: 0, error: getErrorMessage(e) }
    }
    credentialTestResults = { ...credentialTestResults, [cred.id]: res }
    if (res.ok) {
      toast.success(`Key "${cred.label || 'Unnamed'}" works · ${res.latency_ms}ms`)
    } else {
      toast.error(`Key "${cred.label || 'Unnamed'}" failed: ${res.error}`)
    }
    flashTimer('cred:' + cred.id, () => {
      const next = { ...credentialTestResults }
      delete next[cred.id]
      credentialTestResults = next
    })
  }

  function onCredDragStart(e: DragEvent, cred: Credential): void {
    dragCredId = cred.id
    e.dataTransfer?.setData('text/plain', cred.id)
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }

  function onCredDragOver(e: DragEvent): void {
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }

  async function onCredDrop(e: DragEvent, target: Credential): Promise<void> {
    e.preventDefault()
    const dragId = dragCredId || e.dataTransfer?.getData('text/plain') || ''
    dragCredId = ''
    if (!dragId || dragId === target.id) return
    const list = [...credentials]
    const from = list.findIndex((c) => c.id === dragId)
    const to = list.findIndex((c) => c.id === target.id)
    if (from < 0 || to < 0) return
    const [moved] = list.splice(from, 1)
    list.splice(to, 0, moved)
    credentials = list
    try {
      await api.credentials.reorder(providerId, list.map((c) => c.id))
      await reloadCredentials()
    } catch (err) {
      error = getErrorMessage(err)
      await reloadCredentials()
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

  async function testModel(m: ProviderModel): Promise<void> {
    modelTestResults = { ...modelTestResults, [m.name]: 'loading' }
    let res: TestResult
    try {
      res = await api.models.test(`${providerId}/${m.name}`)
    } catch (e) {
      res = { ok: false, latency_ms: 0, error: getErrorMessage(e) }
    }
    modelTestResults = { ...modelTestResults, [m.name]: res }
    if (res.ok) {
      toast.success(`${m.name} works · ${res.latency_ms}ms`)
    } else {
      toast.error(`${m.name} failed: ${res.error}`)
    }
    flashTimer('model:' + m.name, () => {
      const next = { ...modelTestResults }
      delete next[m.name]
      modelTestResults = next
    })
  }

  async function testAllModels(): Promise<void> {
    testingAll = true
    try {
      for (const m of models) {
        if (m.disabled) continue
        await testModel(m)
      }
    } finally {
      testingAll = false
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
      title: 'Delete custom model',
      message: `Remove "${m.name}" from this provider?`,
      severity: 'medium',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true,
    })
    if (!confirmed) return
    try {
      await api.models.deleteOverride(providerId, m.name)
      await reloadModels()
    } catch (e) {
      modelsError = getErrorMessage(e)
    }
  }

  async function copyModelId(name: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(`${providerId}/${name}`)
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function testIcon(res: TestResult | 'loading' | undefined, idleTitle: string): { icon: string; cls: string; title: string } {
    if (!res) return { icon: 'network_check', cls: '', title: idleTitle }
    if (res === 'loading') return { icon: 'progress_activity', cls: 'spin', title: 'Testing…' }
    if (res.ok) return { icon: 'check_circle', cls: 'icon-ok', title: `OK · ${res.latency_ms}ms` }
    return { icon: 'error', cls: 'icon-fail', title: res.error ?? 'Failed' }
  }
</script>

{#if loading}
  <div class="empty">Loading…</div>
{:else if !provider}
  <div class="empty">
    <p>Provider not found.</p>
    <button class="btn btn-secondary" onclick={back}>Back to providers</button>
  </div>
{:else}
  <div class="detail-header">
    <button class="btn-icon" onclick={back} aria-label="Back to providers" title="Back to providers">
      <span class="icon">arrow_back</span>
    </button>
    {#if provider.icon_url}
      <img src={provider.icon_url} alt={provider.name} class="provider-icon" use:squircle={12} />
    {:else}
      <span class="icon provider-icon-fallback">cloud</span>
    {/if}
    <div class="provider-title">
      <h1>{provider.name}</h1>
      <p>{provider.type_key}</p>
    </div>
    <div class="header-actions">
      {#if !provider.is_ui_readonly}
        <button class="btn btn-secondary" onclick={openEditProvider} use:squircle={12}>
          <span class="icon">edit</span>
          Edit
        </button>
        <button class="btn btn-secondary danger" onclick={deleteProvider} use:squircle={12}>
          <span class="icon">delete</span>
          Delete
        </button>
      {/if}
    </div>
  </div>

  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  <section class="section">
    <div class="section-header">
      <h2>API Keys</h2>
      <button class="btn btn-primary" onclick={openAddCredential} use:squircle={12}>
        <span class="icon">add</span>
        Add key
      </button>
    </div>
    {#if credentials.length === 0}
      <div class="empty-state">No keys yet. Add one to route traffic to this provider.</div>
    {:else}
      <div class="table" use:squircle={18}>
        <div class="table-row table-head">
          <span class="col-priority">#</span>
          <span class="col-label">Name</span>
          <span class="col-actions">Actions</span>
        </div>
        {#each credentials as cred, i (cred.id)}
          {@const ti = testIcon(credentialTestResults[cred.id], 'Test key')}
          <div
            class="table-row"
            class:row-disabled={cred.disabled}
            draggable="true"
            ondragstart={(e) => onCredDragStart(e, cred)}
            ondragover={onCredDragOver}
            ondrop={(e) => onCredDrop(e, cred)}
            role="listitem"
          >
            <span class="col-priority">
              <span class="icon drag-handle" title="Drag to reorder">drag_indicator</span>
              {i + 1}
            </span>
            <span class="col-label">
              {cred.label || 'Unnamed'}
              {#if cred.is_expired}
                <span class="badge badge-red">Expired</span>
              {/if}
            </span>
            <span class="col-actions">
              <Switch
                checked={!cred.disabled}
                ariaLabel="Enable key"
                onchange={(v) => toggleCredential(cred, v)}
              />
              <button
                class="btn-icon"
                onclick={() => testCredential(cred)}
                disabled={credentialTestResults[cred.id] === 'loading'}
                aria-label={ti.title}
                title={ti.title}
              >
                <span class="icon {ti.cls}">{ti.icon}</span>
              </button>
              <button class="btn-icon" onclick={() => openEditCredential(cred)} aria-label="Edit key" title="Edit key">
                <span class="icon">edit</span>
              </button>
              <button class="btn-icon" onclick={() => deleteCredential(cred)} aria-label="Delete key" title="Delete key">
                <span class="icon">delete</span>
              </button>
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  <section class="section">
    <div class="proxy-head">
      <h2>Proxy</h2>
      <div class="proxy-head-right">
        <SegmentedControl
          bind:value={proxyMode}
          options={[
            { value: 'disabled', label: 'Disabled' },
            { value: 'auto', label: 'Auto' },
            { value: 'manual', label: 'Manual' },
          ]}
          ariaLabel="Proxy mode"
          onchange={() => saveProxyConfig()}
        />
        {#if savingProxy}
          <span class="icon proxy-state spin">progress_activity</span>
        {:else if proxySaved}
          <span class="icon proxy-state icon-ok">check_circle</span>
        {/if}
      </div>
    </div>
    <p class="form-hint proxy-hint">
      {#if proxyMode === 'disabled'}
        Direct connection, unless the provider's plugin forces a proxy on location mismatch.
      {:else if proxyMode === 'auto'}
        Route through the best pooled proxy matching the plugin's location preference.
      {:else}
        Route through the proxies you select below (first alive wins).
      {/if}
    </p>
    {#if proxyMode === 'manual'}
      {#if poolManual.length === 0}
        <p class="form-hint">No manual proxies in the pool. Add them on the Proxies page.</p>
      {:else}
        <div class="proxy-pick-list">
          {#each poolManual as p (p.id)}
            <button
              type="button"
              class="chip proxy-pick"
              class:chip-blue={proxyIds[p.id]}
              class:chip-neutral={!proxyIds[p.id]}
              class:cap-off={!proxyIds[p.id]}
              aria-pressed={!!proxyIds[p.id]}
              onclick={() => { proxyIds = { ...proxyIds, [p.id]: !proxyIds[p.id] }; saveProxyConfig() }}
              use:squircle={8}
            >
              {p.url}{p.country ? ` · ${p.country}` : ''}{p.alive ? '' : ' · dead'}
            </button>
          {/each}
        </div>
      {/if}
    {/if}
  </section>

  <section class="section">
    <div class="section-header">
      <h2>Available models</h2>
    </div>
    <div class="models-toolbar" use:measure>
      {#if toolbarStage !== 'compact'}
        <input
          class="search-input"
          type="text"
          placeholder="Filter models…"
          bind:value={modelSearch}
          use:squircle={12}
        />
      {:else if searchOpen}
        <div class="search-expand">
          <!-- svelte-ignore a11y_autofocus -->
          <input
            class="search-input"
            type="text"
            placeholder="Filter models…"
            bind:value={modelSearch}
            autofocus
            use:squircle={12}
          />
          <button class="btn-icon" aria-label="Search" title="Search">
            <span class="icon">search</span>
          </button>
          <button class="btn-icon" onclick={() => { searchOpen = false }} aria-label="Close search" title="Close search">
            <span class="icon">close</span>
          </button>
        </div>
      {:else}
        <button class="btn-icon search-open-btn" onclick={() => { searchOpen = true }} aria-label="Open search" title="Filter models">
          <span class="icon">search</span>
        </button>
      {/if}
      {#if toolbarStage !== 'compact'}
        <SegmentedControl
          bind:value={modelFilter}
          options={[
            { value: 'all', label: 'All' },
            { value: 'enabled', label: 'Enabled' },
            { value: 'disabled', label: 'Disabled' },
          ]}
          ariaLabel="Model visibility filter"
        />
      {/if}
      <div class="toolbar-actions">
        {#if toolbarStage === 'full'}
          <button class="btn btn-secondary" onclick={importModels} use:squircle={12}>
            <span class="icon">download</span>
            Import from /models
          </button>
          <button class="btn btn-secondary" onclick={testAllModels} disabled={testingAll} use:squircle={12}>
            <span class="icon">network_check</span>
            {testingAll ? 'Testing…' : 'Test all'}
          </button>
        {:else}
          <ActionDropdown triggerIcon="more_vert" label="More actions" actions={overflowActions} onaction={handleOverflowAction} />
        {/if}
      </div>
    </div>
    {#if modelsError}
      <div class="error-msg">{modelsError}</div>
    {:else if filteredModels.length === 0}
      <div class="empty-state">
        {models.length === 0 ? 'No models reported by this provider.' : 'No models match the filter.'}
      </div>
    {:else}
      {@render modelsTable()}
      {@render modelsCards()}
    {/if}
  </section>

  <section class="section">
    <div class="section-header">
      <h2>Add custom model</h2>
    </div>
    <div class="custom-model-form">
      <input class="search-input" type="text" placeholder="Model id (e.g. my-model-v1)" bind:value={customId} use:squircle={12} />
      <input class="search-input" type="text" placeholder="Display name" bind:value={customName} use:squircle={12} />
      <button class="btn btn-secondary" onclick={probeCustomCapabilities} disabled={!customId.trim() || probingCustom} use:squircle={12}>
        <span class="icon" class:spin={probingCustom}>{probingCustom ? 'progress_activity' : 'fact_check'}</span>
        Check
      </button>
      <button class="btn btn-primary" onclick={addCustomModel} disabled={!customId.trim() || addingCustom} use:squircle={12}>
        <span class="icon">add</span>
        Add model
      </button>
    </div>
    {#if customProbeNote}
      <p class="form-hint">{customProbeNote}</p>
    {/if}
    <div class="cap-picker">
      {#each KNOWN_CAPABILITIES as cap}
        {@const meta = CAPABILITY_META[cap]}
        <button
          type="button"
          class="chip cap-toggle {meta?.cls ?? 'chip-neutral'}"
          class:cap-off={!customCaps[cap]}
          aria-pressed={!!customCaps[cap]}
          onclick={() => { customCaps = { ...customCaps, [cap]: !customCaps[cap] } }}
          use:squircle={8}
        >
          {#if meta?.icon}<span class="icon">{meta.icon}</span>{/if}
          {meta?.label ?? cap}
        </button>
      {/each}
    </div>
    <p class="form-hint">For models the provider does not list in /models.</p>
  </section>
{/if}

{#snippet modelContext(m: ProviderModel)}
  <span class="ctx-text">
    {#if m.context_window}
      <span title="Context window — up to {m.context_window.toLocaleString()} input tokens">{(m.context_window / 1000).toFixed(0)}k context</span>
    {/if}
    {#if m.max_tokens}
      <span title="Max output — up to {m.max_tokens.toLocaleString()} tokens per response">{(m.max_tokens / 1000).toFixed(0)}k output</span>
    {/if}
  </span>
{/snippet}

{#snippet modelCaps(m: ProviderModel)}
  {#if m.custom}
    <span class="chip chip-teal" title="Added manually, not listed by the provider">custom</span>
  {/if}
  {#each m.capabilities as cap}
    {@const meta = CAPABILITY_META[cap]}
    {@const hint = cap === 'reasoning' && meta && m.reasoning?.supported_efforts?.length
      ? `${meta.hint} (effort: ${[...m.reasoning.supported_efforts].reverse().join(', ')})`
      : (meta?.hint ?? cap)}
    <span class="chip {meta?.cls ?? 'chip-neutral'}" title={hint}>
      {#if meta?.icon}<span class="icon">{meta.icon}</span>{/if}
      {meta?.label ?? cap}
    </span>
  {/each}
{/snippet}

{#snippet modelActions(m: ProviderModel)}
  {@const ti = testIcon(modelTestResults[m.name], 'Test model')}
  <span class="model-actions">
    <Switch
      checked={!m.disabled}
      ariaLabel="Enable model"
      onchange={(v) => toggleModel(m, v)}
    />
    <button
      class="btn-icon"
      onclick={() => testModel(m)}
      disabled={modelTestResults[m.name] === 'loading'}
      aria-label={ti.title}
      title={ti.title}
    >
      <span class="icon {ti.cls}">{ti.icon}</span>
    </button>
    {#if m.custom}
      <button class="btn-icon" onclick={() => deleteCustomModel(m)} aria-label="Delete custom model" title="Delete custom model">
        <span class="icon">delete</span>
      </button>
    {/if}
  </span>
{/snippet}

{#snippet modelsTable()}
  <div class="table models-table" use:squircle={18}>
    <div class="table-row model-row table-head">
      <span class="mcol-id">Model</span>
      <span class="mcol-ctx">Context</span>
      <span class="mcol-caps">Capabilities</span>
      <span class="mcol-actions">Actions</span>
    </div>
    {#each filteredModels as m (m.name)}
      <div class="table-row model-row" class:row-disabled={m.disabled}>
        <span class="mcol-id">
          <span class="model-display">{m.display_name || m.name}</span>
          <span class="model-id">
            {m.name}
            <button class="btn-icon copy-btn" onclick={() => copyModelId(m.name)} aria-label="Copy model id" title="Copy model id">
              <span class="icon">content_copy</span>
            </button>
          </span>
        </span>
        <span class="mcol-ctx model-meta">{@render modelContext(m)}</span>
        <span class="mcol-caps model-meta">{@render modelCaps(m)}</span>
        <span class="mcol-actions">{@render modelActions(m)}</span>
      </div>
    {/each}
  </div>
{/snippet}

{#snippet modelsCards()}
  <div class="models-grid">
    {#each filteredModels as m (m.name)}
      <div class="model-card" class:card-disabled={m.disabled} use:squircle={18}>
        <div class="model-card-top">
          <div>
            <div class="model-display">{m.display_name || m.name}</div>
            <span class="model-id">
              {m.name}
              <button class="btn-icon copy-btn" onclick={() => copyModelId(m.name)} aria-label="Copy model id" title="Copy model id">
                <span class="icon">content_copy</span>
              </button>
            </span>
          </div>
          {@render modelActions(m)}
        </div>
        <div class="model-meta">{@render modelContext(m)}{@render modelCaps(m)}</div>
      </div>
    {/each}
  </div>
{/snippet}

<style>
  .detail-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 32px;
  }
  .provider-icon {
    width: 40px;
    height: 40px;
    border-radius: 10px;
    object-fit: contain;
  }
  .provider-icon-fallback {
    font-size: 40px;
    color: var(--color-text-soft);
  }
  .provider-title h1 {
    font-size: 20px;
    font-weight: 600;
    margin: 0;
  }
  .provider-title p {
    margin: 0;
    color: var(--color-text-soft);
    font-size: 13px;
  }
  .header-actions {
    margin-left: auto;
    display: flex;
    gap: 8px;
  }
  .btn.danger {
    color: var(--color-error-text);
  }
  .section {
    margin-bottom: 40px;
  }
  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 16px;
  }
  .section-header h2 {
    font-size: 16px;
    font-weight: 600;
    margin: 0;
  }
  .empty-state {
    padding: 32px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
    border: 1px dashed var(--color-outline-light);
    border-radius: var(--radius-lg);
  }
  .table {
    border-radius: var(--radius-lg);
    overflow: hidden;
  }
  .table-row {
    display: grid;
    grid-template-columns: 64px minmax(0, 1fr) auto;
    align-items: center;
    padding: 10px 16px;
    gap: 12px;
    background: var(--color-surface-container-high);
  }
  .table-row + .table-row {
    border-top: 1px solid var(--color-outline-soft);
  }
  .table-row[draggable='true'] {
    cursor: grab;
  }
  .table-row[draggable='true']:active {
    cursor: grabbing;
  }
  .table-head {
    background: var(--color-surface-container-highest);
    font-size: 12px;
    font-weight: 600;
    color: var(--color-text-soft);
    cursor: default;
  }
  .table-head .col-actions,
  .table-head .mcol-actions {
    justify-content: flex-start;
  }
  .row-disabled {
    opacity: 0.55;
  }
  .col-priority {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--color-text-soft);
  }
  .col-label {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .drag-handle {
    font-size: 18px;
    color: var(--color-text-disabled);
  }
  .col-actions {
    display: flex;
    gap: 4px;
    align-items: center;
    justify-content: flex-end;
  }
  .models-toolbar {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
  }
  .search-expand {
    display: flex;
    align-items: center;
    gap: 4px;
    animation: search-in 0.28s cubic-bezier(0.3, 1.15, 0.5, 1);
  }
  .search-expand .search-input {
    width: 240px;
  }
  @keyframes search-in {
    from {
      opacity: 0;
      transform: translateX(-12px);
    }
  }
  .search-open-btn {
    transition: transform 0.12s ease;
  }
  .search-open-btn:active {
    transform: scale(0.88);
  }
  .toolbar-actions {
    margin-left: auto;
    display: flex;
    gap: 8px;
  }
  .search-input {
    flex: 0 1 280px;
    height: 36px;
    padding: 0 12px;
    border: none;
    border-radius: var(--radius-md);
    background: var(--color-surface-container-highest);
    color: var(--color-text);
    font-family: inherit;
    font-size: 14px;
  }
  .search-input:focus {
    outline: none;
    box-shadow: inset 0 0 0 2px var(--color-accent);
  }
  .models-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 12px;
  }
  /* Desktop: models as a table; mobile: cards */
  .models-grid { display: none; }
  .model-row {
    grid-template-columns: minmax(0, 1.2fr) minmax(0, 0.7fr) minmax(0, 1.4fr) auto;
  }
  .mcol-id {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .model-display {
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mcol-caps, .mcol-ctx { margin-top: 0; }
  .ctx-text {
    display: flex;
    flex-direction: column;
    gap: 1px;
    font-size: 13px;
    color: var(--color-text-soft);
    white-space: nowrap;
  }
  .mcol-actions { display: flex; justify-content: flex-end; }
  @media (max-width: 860px) {
    .models-table { display: none; }
    .models-grid { display: grid; }
  }
  .model-card {
    border-radius: var(--radius-lg);
    padding: 14px 16px;
    background: var(--color-surface-container-high);
  }
  .card-disabled {
    opacity: 0.55;
  }
  .model-card-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .model-id {
    display: flex;
    align-items: center;
    gap: 2px;
    font-family: 'DM Mono', monospace;
    font-size: 13px;
    color: var(--color-text);
    word-break: break-all;
  }
  .copy-btn .icon {
    font-size: 15px;
  }
  .model-actions {
    display: flex;
    gap: 6px;
    align-items: center;
    flex-shrink: 0;
  }
  .model-meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-top: 10px;
  }
  .custom-model-form {
    display: flex;
    gap: 12px;
  }
  .cap-picker {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 12px;
  }
  .cap-toggle {
    border: none;
    cursor: pointer;
    font-family: inherit;
    transition:
      opacity 0.15s ease,
      transform 0.12s ease;
  }
  .cap-toggle:active {
    transform: scale(0.94);
  }
  .cap-off {
    opacity: 0.35;
  }
  .form-hint {
    margin-top: 8px;
    font-size: 13px;
    color: var(--color-text-soft);
  }
  .empty {
    padding: 48px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }
  @keyframes icon-spin {
    to { transform: rotate(360deg); }
  }
  .spin {
    animation: icon-spin 1s linear infinite;
  }
  .icon-ok {
    color: var(--color-success-text);
  }
  .icon-fail {
    color: var(--color-error-text);
  }

  @media (max-width: 768px) {
    .detail-header {
      flex-wrap: wrap;
    }
    .header-actions {
      width: 100%;
      justify-content: flex-end;
    }
    .models-toolbar {
      flex-wrap: wrap;
    }
    .toolbar-actions {
      margin-left: 0;
      width: 100%;
      justify-content: flex-end;
    }
    .search-input {
      flex: 1 1 100%;
    }
  .proxy-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
  }
  .proxy-head h2 {
    font-size: 16px;
    font-weight: 600;
    margin: 0;
  }
  .proxy-head-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .proxy-hint {
    text-align: right;
  }
  .proxy-state {
    font-size: 20px;
  }
  .proxy-pick-list {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .proxy-pick {
    border: none;
    cursor: pointer;
    font-family: inherit;
    transition:
      opacity 0.15s ease,
      transform 0.12s ease;
  }
  .proxy-pick:active {
    transform: scale(0.94);
  }
  .custom-model-form {
      flex-direction: column;
    }
  }
  @media (max-width: 520px) {
    .drag-handle {
      display: none;
    }
    .table-row {
      grid-template-columns: 24px minmax(0, 1fr) auto;
      padding: 10px 12px;
    }
    .col-actions {
      gap: 2px;
    }
  }
</style>
