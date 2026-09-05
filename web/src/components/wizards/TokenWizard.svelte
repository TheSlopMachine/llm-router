<script lang="ts">
  import { onMount } from 'svelte'
  import { untrack } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import type { Token, Provider, ProviderModels } from '../../lib/types'
  import type { ModalButton, StepperConfig } from '../../lib/modal.svelte'

  let {
    providers,
    editingToken = null,
    cloningToken = null,
    onComplete,
    updateButtons,
    updateTitle,
    updateSubtitle,
    updateStepper,
    updateFooterHint,
    closeModal
  } = $props<{
    providers: Provider[]
    editingToken?: Token | null
    cloningToken?: Token | null
    onComplete: (result: { token?: string }) => void
    updateButtons: (buttons: ModalButton[]) => void
    updateTitle: (title: string) => void
    updateSubtitle: (subtitle: string) => void
    updateStepper: (stepper: StepperConfig | null) => void
    updateFooterHint: (hint: string) => void
    closeModal: () => void
  }>()

  let wizardStep: number = $state(1)
  let wizardLoading: boolean = $state(false)
  let error: string = $state('')

  let tokenName: string = $state('')
  let allowAllProviders: boolean = $state(false)
  let selectedProviders: Set<string> = $state(new Set())
  let providerModels: ProviderModels[] = $state([])
  let allowAllModels: boolean = $state(false)
  let selectedModels: Set<string> = $state(new Set())
  let allowAllCredentials: boolean = $state(false)
  let selectedCredentials: Set<string> = $state(new Set())
  let allCredentials: Array<{ id: string; provider_id: string; provider_name: string; label: string; is_expired: boolean; updated_at?: string }> = $state([])

  let searchModels: string = $state('')
  let searchAccounts: string = $state('')
  let view = $state<'wizard' | 'success'>('wizard')
  let createdToken: string | null = $state(null)
  let copied: boolean = $state(false)
  let copyTimeout: ReturnType<typeof setTimeout> | undefined = $state(undefined)

  let isEditMode = $derived(!!editingToken)
  let baseTitle = $derived(editingToken ? 'Edit token' : cloningToken ? 'Clone token' : 'New token')

  // subtitle: token name once typed, fallback Step X of 3
  let subtitle = $derived(view === 'success' ? 'Token created successfully' : tokenName.trim() ? tokenName.trim() : `Step ${wizardStep} of 3`)

  let stepperConfig: StepperConfig | null = $derived(
    view === 'success'
      ? null
      : { current: wizardStep, total: 3, labels: ['Name & access', 'Models', 'Accounts'] }
  )

  onMount(async () => {
    const source = editingToken ?? cloningToken
    if (source) {
      const r: any = source.rules || {}
      tokenName = cloningToken ? `${source.name} (copy)` : source.name
      allowAllProviders = !!r.allow_all_providers
      selectedProviders = new Set(r.allowed_providers || [])
      allowAllModels = !!r.allow_all_models
      selectedModels = new Set(r.allowed_models || [])
      allowAllCredentials = !!r.allow_all_credentials
      selectedCredentials = new Set(r.allowed_credentials || [])
    }

    try {
      const creds: any = await api.credentials.list()
      allCredentials = Array.isArray(creds) ? creds : (creds?.credentials ?? creds ?? [])
      if (!Array.isArray(allCredentials)) allCredentials = []
    } catch (_) {
      allCredentials = []
    }

    syncChrome()
  })

  function getEligibleProviderIds(): Set<string> {
    if (!allowAllProviders) return new Set(selectedProviders)
    if (!allowAllModels && selectedModels.size > 0) {
      const types = new Set<string>()
      for (const m of selectedModels) {
        const pre = m.split('/')[0]
        if (pre) types.add(pre)
      }
      const ids = providers.filter((p: Provider) => types.has(p.type)).map((p: Provider) => p.id)
      if (ids.length > 0) return new Set(ids)
    }
    return new Set(providers.map((p: Provider) => p.id))
  }

  let eligibleProviderIds = $derived(getEligibleProviderIds())
  let eligibleProviders = $derived(providers.filter((p: Provider) => eligibleProviderIds.has(p.id)))
  let eligibleCredentialsGrouped = $derived(
    eligibleProviders.map((p: Provider) => ({
      provider: p,
      creds: allCredentials.filter((c) => c.provider_id === p.id)
    }))
  )

  // validation — derived only, no effect writes
  let hasAnyModelAvailable = $derived(providerModels.some((pm: ProviderModels) => (pm.models?.length ?? 0) > 0))
  let hasAnyCredentialAvailable = $derived(eligibleCredentialsGrouped.some((g: { creds: typeof allCredentials }) => g.creds.length > 0))

  let step1Valid = $derived(!!tokenName.trim() && (allowAllProviders || selectedProviders.size > 0))
  let step1Hint = $derived.by(() => {
    if (!tokenName.trim()) return 'Enter a token name.'
    if (!allowAllProviders && selectedProviders.size === 0) return 'Select at least one provider or enable Allow all.'
    return ''
  })

  let step2Valid = $derived.by(() => {
    if (allowAllModels) return true
    if (!hasAnyModelAvailable) return true
    return selectedModels.size > 0
  })
  let step2Hint = $derived.by(() => {
    if (allowAllModels) return ''
    if (!hasAnyModelAvailable) return ''
    if (selectedModels.size === 0) return 'Select at least one model or enable Allow all.'
    return ''
  })

  let step3Valid = $derived.by(() => {
    if (allowAllCredentials) return true
    if (!hasAnyCredentialAvailable) return true
    return selectedCredentials.size > 0
  })
  let step3Hint = $derived.by(() => {
    if (allowAllCredentials) return ''
    if (!hasAnyCredentialAvailable) return ''
    if (selectedCredentials.size === 0) return 'Select at least one account or enable Allow all.'
    return ''
  })

  let currentHint = $derived(view === 'success' ? '' : wizardStep === 1 ? step1Hint : wizardStep === 2 ? step2Hint : step3Hint)
  let currentValid = $derived(view === 'success' ? true : wizardStep === 1 ? step1Valid : wizardStep === 2 ? step2Valid : step3Valid)

  function providerDescription(p: Provider): string {
    if (p.auth_type === 'api_key') return 'API key access'
    if (p.auth_type === 'oauth2') return 'OAuth access'
    if (p.auth_type === 'custom') return 'Custom access'
    if (p.auth_type) return `${p.auth_type} access`
    return `${p.type} access`
  }

  function modelTraits(model: string): string[] {
    const lower = model.toLowerCase()
    const traits: string[] = []
    if (lower.includes('preview') || lower.includes('beta') || lower.includes('experimental')) traits.push('Preview')
    if (lower.includes('vision') || lower.includes('image')) traits.push('Image')
    if (lower.includes('audio') || lower.includes('whisper') || lower.includes('tts')) traits.push('Audio')
    if (lower.includes('agent') || lower.includes('tool') || lower.includes('function')) traits.push('Agentic')
    return traits.slice(0, 2)
  }

  function traitBadgeClass(trait: string): string {
    if (trait === 'Preview') return 'badge-yellow'
    if (trait === 'Image') return 'badge-blue'
    if (trait === 'Audio') return 'badge-green'
    if (trait === 'Agentic') return 'badge badge-blue'
    return 'badge-blue'
  }

  function formatRelativeTime(iso?: string): string {
    if (!iso) return '—'
    const d = new Date(iso)
    if (isNaN(d.getTime())) return '—'
    const diff = Date.now() - d.getTime()
    const sec = Math.floor(diff / 1000)
    const min = Math.floor(sec / 60)
    const hr = Math.floor(min / 60)
    const day = Math.floor(hr / 24)
    if (sec < 60) return 'Just now'
    if (min < 60) return `${min}m ago`
    if (hr < 24) return `${hr}h ago`
    if (day < 30) return `${day}d ago`
    return d.toLocaleDateString()
  }

  function filteredModels(models: string[], query: string): string[] {
    if (!query.trim()) return models
    const q = query.toLowerCase()
    return models.filter((m) => m.toLowerCase().includes(q))
  }

  function selectAllModelsInGroup(pm: ProviderModels, select: boolean): void {
    const s = new Set(selectedModels)
    for (const m of pm.models ?? []) {
      const fullId = `${pm.provider_type}/${m}`
      if (filteredModels([m], searchModels).length === 0 && searchModels.trim()) continue
      if (select) s.add(fullId)
      else s.delete(fullId)
    }
    selectedModels = s
  }

  function isGroupAllSelected(pm: ProviderModels): boolean {
    const ms = pm.models ?? []
    if (ms.length === 0) return false
    const filtered = filteredModels(ms, searchModels)
    if (filtered.length === 0) return false
    return filtered.every((m) => selectedModels.has(`${pm.provider_type}/${m}`))
  }

  function groupSelectedCount(pm: ProviderModels): number {
    let c = 0
    for (const m of pm.models ?? []) {
      if (selectedModels.has(`${pm.provider_type}/${m}`)) c++
    }
    return c
  }

  function selectAllCredsInGroup(group: { provider: Provider; creds: typeof allCredentials }, select: boolean): void {
    const s = new Set(selectedCredentials)
    const q = searchAccounts.toLowerCase()
    const filtered = !searchAccounts.trim() ? group.creds : group.creds.filter((c) => c.label.toLowerCase().includes(q) || c.id.toLowerCase().includes(q))
    for (const c of filtered) {
      if (select) s.add(c.id)
      else s.delete(c.id)
    }
    selectedCredentials = s
  }

  function isCredGroupAllSelected(group: { provider: Provider; creds: typeof allCredentials }): boolean {
    if (group.creds.length === 0) return false
    const q = searchAccounts.toLowerCase()
    const filtered = !searchAccounts.trim() ? group.creds : group.creds.filter((c) => c.label.toLowerCase().includes(q) || c.id.toLowerCase().includes(q))
    if (filtered.length === 0) return false
    return filtered.every((c) => selectedCredentials.has(c.id))
  }

  function syncChrome(): void {
    untrack(() => {
      updateTitle(baseTitle)
      updateSubtitle(subtitle)
      updateStepper(stepperConfig)
      updateFooterHint(currentHint)

      if (view === 'success') {
        updateButtons([
          { label: 'Create another', variant: 'secondary', onClick: resetWizard },
          { label: 'Done', variant: 'primary', onClick: handleDone }
        ])
        return
      }

      if (wizardStep === 1) {
        updateButtons([
          { label: 'Cancel', variant: 'secondary', onClick: closeModal },
          { label: 'Next', variant: 'primary', onClick: goToStep2, disabled: !currentValid, loading: wizardLoading }
        ])
      } else if (wizardStep === 2) {
        updateButtons([
          { label: 'Back', variant: 'secondary', onClick: goBackToStep1 },
          { label: 'Next', variant: 'primary', onClick: goToStep3, disabled: !currentValid, loading: wizardLoading }
        ])
      } else {
        updateButtons([
          { label: 'Back', variant: 'secondary', onClick: goBackToStep2 },
          {
            label: isEditMode ? 'Update token' : 'Create token',
            variant: 'primary',
            onClick: submit,
            disabled: !currentValid,
            loading: wizardLoading
          }
        ])
      }
    })
  }

  function goBackToStep1(): void {
    wizardStep = 1
    error = ''
    syncChrome()
  }

  function goBackToStep2(): void {
    wizardStep = 2
    error = ''
    syncChrome()
  }

  function toggleProvider(id: string): void {
    const s = new Set(selectedProviders)
    s.has(id) ? s.delete(id) : s.add(id)
    selectedProviders = s
  }

  function toggleModel(fullId: string): void {
    const s = new Set(selectedModels)
    s.has(fullId) ? s.delete(fullId) : s.add(fullId)
    selectedModels = s
  }

  function toggleCredential(id: string): void {
    const s = new Set(selectedCredentials)
    s.has(id) ? s.delete(id) : s.add(id)
    selectedCredentials = s
  }

  async function goToStep2(): Promise<void> {
    error = ''
    if (!tokenName.trim()) {
      error = 'Token name is required.'
      return
    }
    if (!allowAllProviders && selectedProviders.size === 0) {
      error = 'Select at least one provider or allow all.'
      return
    }
    wizardLoading = true
    syncChrome()
    try {
      const ids = allowAllProviders ? providers.map((p: Provider) => p.id) : [...selectedProviders]
      if (ids.length === 0) {
        providerModels = []
      } else {
        const result: any = await api.models.list(ids)
        providerModels = result?.providers || result?.data || []
        if (!Array.isArray(providerModels)) providerModels = []
      }
      const availableTypes = new Set(providerModels.map((p: any) => p.provider_type))
      for (const m of [...selectedModels]) {
        const t = m.split('/')[0]
        if (!availableTypes.has(t)) selectedModels.delete(m)
      }
      selectedModels = new Set(selectedModels)
      wizardStep = 2
      error = ''
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      wizardLoading = false
      syncChrome()
    }
  }

  async function goToStep3(): Promise<void> {
    error = ''
    if (!allowAllModels && !step2Valid) {
      error = step2Hint || 'Select at least one model or allow all.'
      return
    }
    wizardLoading = true
    syncChrome()
    try {
      if (allCredentials.length === 0) {
        try {
          const creds: any = await api.credentials.list()
          allCredentials = Array.isArray(creds) ? creds : []
        } catch (_) {}
      }
      const eligible = getEligibleProviderIds()
      for (const cid of [...selectedCredentials]) {
        const cred = allCredentials.find((c) => c.id === cid)
        if (!cred || !eligible.has(cred.provider_id)) selectedCredentials.delete(cid)
      }
      selectedCredentials = new Set(selectedCredentials)
      wizardStep = 3
      error = ''
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      wizardLoading = false
      syncChrome()
    }
  }

  async function submit(): Promise<void> {
    error = ''
    wizardLoading = true
    syncChrome()

    const payload = {
      name: tokenName,
      rules: {
        allowed_providers: allowAllProviders ? null : [...selectedProviders],
        allow_all_providers: allowAllProviders,
        allowed_models: allowAllModels ? null : [...selectedModels],
        allow_all_models: allowAllModels,
        allowed_credentials: allowAllCredentials ? null : [...selectedCredentials],
        allow_all_credentials: allowAllCredentials
      }
    }

    try {
      if (editingToken) {
        await api.tokens.update(editingToken.id, payload as any)
        onComplete({})
        closeModal()
      } else {
        const result: any = await api.tokens.create(payload as any)
        const tokenStr: string | null = result?.token ?? result?.Token ?? result?.token_hash ?? null
        createdToken = tokenStr
        if (tokenStr) {
          view = 'success'
          error = ''
        } else {
          // fallback: if backend didn't return token string, just complete
          onComplete({ token: tokenStr ?? undefined })
          closeModal()
          return
        }
        syncChrome()
      }
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      wizardLoading = false
      if (view !== 'success') syncChrome()
      else {
        // ensure loading cleared and chrome updated for success view
        wizardLoading = false
        syncChrome()
      }
    }
  }

  function handleDone(): void {
    const tokenToReturn = createdToken
    onComplete({ token: tokenToReturn ?? undefined })
    closeModal()
  }

  function resetWizard(): void {
    tokenName = ''
    allowAllProviders = false
    selectedProviders = new Set()
    providerModels = []
    allowAllModels = false
    selectedModels = new Set()
    allowAllCredentials = false
    selectedCredentials = new Set()
    searchModels = ''
    searchAccounts = ''
    error = ''
    createdToken = null
    copied = false
    if (copyTimeout) clearTimeout(copyTimeout)
    wizardStep = 1
    view = 'wizard'
    syncChrome()
  }

  async function copyToken(): Promise<void> {
    if (!createdToken) return
    try {
      await navigator.clipboard.writeText(createdToken)
      copied = true
      if (copyTimeout) clearTimeout(copyTimeout)
      copyTimeout = window.setTimeout(() => (copied = false), 2000)
    } catch (_) {
      // fallback: select via execCommand not needed — show error
      error = 'Copy failed. Please select and copy manually.'
    }
  }

  // Single effect that syncs chrome — parent writes untracked
  $effect(() => {
    void wizardStep
    void tokenName
    void allowAllProviders
    void selectedProviders.size
    void allowAllModels
    void selectedModels.size
    void allowAllCredentials
    void selectedCredentials.size
    void wizardLoading
    void searchModels
    void searchAccounts
    void view
    void createdToken
    void error
    // derived hints/validity consumed via currentHint/currentValid already tracked via above
    void subtitle
    void stepperConfig
    void currentHint
    untrack(() => syncChrome())
  })
</script>

{#if view === 'success'}
  <div class="success-view">
    <div class="success-icon-wrap">
      <span class="icon success-icon">check_circle</span>
    </div>
    <h3 class="success-title">Token created</h3>
    <p class="success-hint">Copy it now — it will not be shown again.</p>

    {#if error}
      <div class="error-msg">{error}</div>
    {/if}

    <div class="token-box">
      <span class="mono token-string">{createdToken}</span>
      <button class="btn btn-secondary btn-sm" onclick={copyToken} aria-label="Copy token">
        <span class="icon" style="font-size: 16px;">{copied ? 'check' : 'content_copy'}</span>
        {copied ? 'Copied!' : 'Copy'}
      </button>
    </div>

    <div class="scope-summary">
      <div class="scope-block">
        <span class="scope-label">Providers</span>
        <span class="scope-value">{allowAllProviders ? 'All providers' : `${selectedProviders.size} provider${selectedProviders.size !== 1 ? 's' : ''}`}</span>
      </div>
      <div class="scope-block">
        <span class="scope-label">Models</span>
        <span class="scope-value">{allowAllModels ? 'All models' : `${selectedModels.size} model${selectedModels.size !== 1 ? 's' : ''}`}</span>
      </div>
      <div class="scope-block">
        <span class="scope-label">Accounts</span>
        <span class="scope-value">{allowAllCredentials ? 'All accounts' : `${selectedCredentials.size} account${selectedCredentials.size !== 1 ? 's' : ''}`}</span>
      </div>
    </div>
  </div>
{:else if wizardStep === 1}
  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  <div class="form-group">
    <label for="token-name">Token name *</label>
    <input id="token-name" type="text" bind:value={tokenName} placeholder="My Application" />
  </div>

  <div class="form-group">
    <label class="switch-row" for="allow-all-providers">
      <span>Allow all providers</span>
      <button
        id="allow-all-providers"
        type="button"
        role="switch"
        aria-checked={allowAllProviders}
        aria-label="Allow all providers"
        class="switch"
        class:on={allowAllProviders}
        onclick={() => (allowAllProviders = !allowAllProviders)}
      >
        <span class="switch-thumb"></span>
      </button>
    </label>
    {#if allowAllProviders}
      <div class="hint">All providers are allowed. The list below is disabled but visible.</div>
    {/if}
  </div>

  <div class="form-group" class:is-disabled={allowAllProviders}>
    <div class="form-label">Providers</div>
    {#if providers.length === 0}
      <div class="muted-placeholder">No providers available.</div>
    {:else}
      <div class="provider-grid">
        {#each providers as p}
          {@const selected = selectedProviders.has(p.id)}
          <button
            type="button"
            class="provider-tile"
            class:selected
            onclick={() => !allowAllProviders && toggleProvider(p.id)}
            disabled={allowAllProviders}
            aria-pressed={selected}
          >
            <div class="tile-head">
              <span class="tile-name">{p.name}</span>
              {#if selected}
                <span class="icon tile-check">check_circle</span>
              {/if}
            </div>
            <span class="tile-desc">{providerDescription(p)} <span class="text-muted">· {p.type}{p.qualifier ? ':' + p.qualifier : ''}</span></span>
          </button>
        {/each}
      </div>
    {/if}
  </div>

{:else if wizardStep === 2}
  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  <div class="form-group">
    <label class="switch-row" for="allow-all-models">
      <span>Allow all models</span>
      <button
        id="allow-all-models"
        type="button"
        role="switch"
        aria-checked={allowAllModels}
        aria-label="Allow all models"
        class="switch"
        class:on={allowAllModels}
        onclick={() => (allowAllModels = !allowAllModels)}
      >
        <span class="switch-thumb"></span>
      </button>
    </label>
    {#if allowAllModels}
      <div class="hint">All models of the selected providers are allowed. The list below is disabled but visible.</div>
    {/if}
  </div>

  <div class="search-wrap">
    <span class="icon search-icon">search</span>
    <input type="text" placeholder="Search models..." bind:value={searchModels} disabled={allowAllModels} />
  </div>

  <div class="model-sections" class:is-disabled={allowAllModels}>
    {#each providerModels as pm}
      <div class="model-section">
        <div class="section-header">
          <span class="section-title">{pm.provider_name}</span>
          <span class="section-count">{groupSelectedCount(pm)} / {pm.models?.length ?? 0}</span>
          <button
            type="button"
            class="btn-link select-all-btn"
            onclick={() => selectAllModelsInGroup(pm, !isGroupAllSelected(pm))}
            disabled={allowAllModels || (pm.models?.length ?? 0) === 0}
          >
            {isGroupAllSelected(pm) ? 'Deselect all' : 'Select all'}
          </button>
          {#if pm.error}
            <span class="badge badge-red">{pm.error}</span>
          {/if}
        </div>

        {#if pm.models?.length}
          {@const filtered = filteredModels(pm.models, searchModels)}
          {#if filtered.length === 0}
            <div class="muted-placeholder">No matches for "{searchModels}"</div>
          {:else}
            <div class="checkbox-list">
              {#each filtered as model}
                {@const fullId = `${pm.provider_type}/${model}`}
                {@const traits = modelTraits(model)}
                <label class="checkbox-item">
                  <input type="checkbox" checked={selectedModels.has(fullId)} onchange={() => toggleModel(fullId)} disabled={allowAllModels} />
                  <span class="mono">{model}</span>
                  {#each traits as t}
                    <span class="badge {traitBadgeClass(t)} badge-sm">{t}</span>
                  {/each}
                </label>
              {/each}
            </div>
          {/if}
        {:else if !pm.error}
          <div class="muted-placeholder">No models available for this provider</div>
        {/if}
      </div>
    {/each}

    {#if providerModels.length === 0}
      <div class="muted-placeholder">No providers selected — go back and select providers.</div>
    {/if}
  </div>

{:else if wizardStep === 3}
  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  <div class="form-group">
    <label class="switch-row" for="allow-all-creds">
      <span>Allow all accounts</span>
      <button
        id="allow-all-creds"
        type="button"
        role="switch"
        aria-checked={allowAllCredentials}
        aria-label="Allow all accounts"
        class="switch"
        class:on={allowAllCredentials}
        onclick={() => (allowAllCredentials = !allowAllCredentials)}
      >
        <span class="switch-thumb"></span>
      </button>
    </label>
    {#if allowAllCredentials}
      <div class="hint">All accounts of the eligible providers are allowed. The list below is disabled but visible.</div>
    {/if}
  </div>

  <div class="search-wrap">
    <span class="icon search-icon">search</span>
    <input type="text" placeholder="Search accounts..." bind:value={searchAccounts} disabled={allowAllCredentials} />
  </div>

  <div class="model-sections" class:is-disabled={allowAllCredentials}>
    {#if allCredentials.length === 0}
      <div class="muted-placeholder">No accounts registered yet.</div>
    {:else if eligibleCredentialsGrouped.length === 0}
      <div class="muted-placeholder">No accounts match the selected providers/models.</div>
    {:else}
      {#each eligibleCredentialsGrouped as group}
        {@const q = searchAccounts.toLowerCase()}
        {@const filtered = !searchAccounts.trim() ? group.creds : group.creds.filter((c: (typeof allCredentials)[number]) => c.label.toLowerCase().includes(q) || c.id.toLowerCase().includes(q))}
        <div class="model-section">
          <div class="section-header">
            <span class="section-title">{group.provider.name} <span class="text-muted">({group.provider.id})</span></span>
            <span class="section-count">{group.creds.filter((c: (typeof allCredentials)[number]) => selectedCredentials.has(c.id)).length} / {group.creds.length}</span>
            <button
              type="button"
              class="btn-link select-all-btn"
              onclick={() => selectAllCredsInGroup(group, !isCredGroupAllSelected(group))}
              disabled={allowAllCredentials || group.creds.length === 0}
            >
              {isCredGroupAllSelected(group) ? 'Deselect all' : 'Select all'}
            </button>
          </div>

          {#if group.creds.length === 0}
            <div class="muted-placeholder">No accounts for this provider</div>
          {:else if filtered.length === 0}
            <div class="muted-placeholder">No matches for "{searchAccounts}"</div>
          {:else}
            <div class="checkbox-list cred-list">
              {#each filtered as cred}
                <label class="checkbox-item cred-item">
                  <input type="checkbox" checked={selectedCredentials.has(cred.id)} onchange={() => toggleCredential(cred.id)} disabled={allowAllCredentials} />
                  <span class="cred-main">
                    <span class="cred-label">{cred.label || 'API Key'}</span>
                    <span class="text-muted cred-meta">{formatRelativeTime(cred.updated_at)} · <span class="mono">{cred.id.slice(0, 8)}…</span></span>
                  </span>
                  {#if cred.is_expired}
                    <span class="badge badge-red badge-sm">expired</span>
                  {/if}
                </label>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
    {/if}
  </div>
{/if}

<style>
  .form-group {
    margin-bottom: 16px;
  }

  .form-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
    margin-bottom: 6px;
  }

  .is-disabled {
    opacity: 0.55;
    pointer-events: none;
  }

  .switch-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    font-size: 14px;
    font-weight: 400;
    color: var(--color-text);
    cursor: pointer;
    user-select: none;
  }

  .switch {
    width: 36px;
    height: 20px;
    border-radius: 9999px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface-container-highest);
    position: relative;
    cursor: pointer;
    padding: 0;
    flex-shrink: 0;
    transition:
      background 0.15s,
      border-color 0.15s;
  }

  .switch.on {
    background: var(--color-text);
    border-color: var(--color-text);
  }

  .switch-thumb {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--color-surface);
    box-shadow: var(--shadow-xs);
    transition: transform 0.15s;
  }

  .switch.on .switch-thumb {
    transform: translateX(16px);
    background: var(--color-surface);
  }

  .hint {
    margin-top: 6px;
    font-size: 12px;
    color: var(--color-text-soft);
  }

  .provider-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 12px;
    margin-top: 8px;
  }

  .provider-tile {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 12px 14px;
    border-radius: 12px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface);
    cursor: pointer;
    text-align: left;
    transition:
      border-color 0.15s,
      background 0.15s;
    height: auto;
    width: 100%;
  }

  .provider-tile:hover:not(:disabled) {
    border-color: var(--color-outline-variant);
    background: var(--color-hover-bg);
  }

  .provider-tile.selected {
    border-color: var(--color-text-soft);
    background: var(--color-hover-bg);
  }

  .tile-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    gap: 8px;
  }

  .tile-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tile-check {
    font-size: 18px;
    color: var(--color-text);
    flex-shrink: 0;
  }

  .tile-desc {
    font-size: 12px;
    color: var(--color-text-soft);
    line-height: 16px;
  }

  .text-muted {
    color: var(--color-text-soft);
  }

  .search-wrap {
    position: relative;
    margin-bottom: 16px;
  }

  .search-wrap input {
    padding-left: 36px;
  }

  .search-icon {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    font-size: 18px;
    color: var(--color-text-soft);
    pointer-events: none;
  }

  .checkbox-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 8px 16px;
    margin-top: 8px;
  }

  .cred-list {
    grid-template-columns: 1fr;
  }

  .checkbox-item {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    cursor: pointer;
    user-select: none;
  }

  .checkbox-item input[type='checkbox'] {
    width: auto;
    cursor: pointer;
  }

  .badge-sm {
    padding: 2px 8px;
    font-size: 10px;
  }

  .model-sections {
    display: flex;
    flex-direction: column;
    gap: 24px;
    margin-top: 16px;
  }

  .model-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-header {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .section-title {
    font-size: 13px;
    font-weight: 500;
    color: var(--color-text);
  }

  .section-count {
    font-size: 12px;
    color: var(--color-text-soft);
  }

  .select-all-btn {
    font-size: 12px;
    padding: 0 8px;
    height: 24px;
  }

  .muted-placeholder {
    padding: 10px 12px;
    border: 1px dashed var(--color-outline-soft);
    border-radius: 8px;
    color: var(--color-text-soft);
    font-size: 12px;
    text-align: center;
  }

  .cred-item {
    padding: 8px 12px;
    border: 1px solid var(--color-outline-soft);
    border-radius: 8px;
    background: var(--color-surface);
  }

  .cred-main {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }

  .cred-label {
    font-size: 13px;
    font-weight: 500;
    color: var(--color-text);
  }

  .cred-meta {
    font-size: 11px;
    display: flex;
    gap: 6px;
    align-items: center;
  }

  /* success view */
  .success-view {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
    padding: 16px 0;
    text-align: center;
  }

  .success-icon-wrap {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    background: rgba(34, 197, 94, 0.1);
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .success-icon {
    font-size: 28px;
    color: var(--color-success-text);
  }

  .success-title {
    font-size: 18px;
    font-weight: 500;
    color: var(--color-text);
    margin: 0;
  }

  .success-hint {
    font-size: 13px;
    color: var(--color-text-soft);
    margin: 0;
  }

  .token-box {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    padding: 12px 14px;
    border-radius: 8px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface-container-highest);
    text-align: left;
  }

  .token-string {
    flex: 1;
    min-width: 0;
    word-break: break-all;
    font-size: 12px;
    color: var(--color-text);
  }

  .scope-summary {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
    width: 100%;
  }

  .scope-block {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 14px 12px;
    border-radius: 12px;
    border: 1px solid var(--color-outline-soft);
    background: var(--color-surface);
    text-align: center;
  }

  .scope-label {
    font-size: 11px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-soft);
  }

  .scope-value {
    font-size: 13px;
    font-weight: 500;
    color: var(--color-text);
  }
</style>
