<script lang="ts">
  import { onMount } from 'svelte'
  import { untrack } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import { formatRelativeTime } from '../../lib/time'
  import { modelTraits, traitBadgeClass, toggleSet } from '../../lib/token-helpers'
  import { t, n } from '../../lib/i18n.svelte'
  import type { Token, Provider, ProviderModels } from '../../lib/types'
  import type { ModalButton, StepperConfig } from '../../lib/modal.svelte'
  import Switch from '../ui/Switch.svelte'
  import SearchField from '../ui/SearchField.svelte'
  import ProviderTileGrid from './token/ProviderTileGrid.svelte'
  import TokenSuccessView from './token/TokenSuccessView.svelte'
  import GroupedChecklist from './token/GroupedChecklist.svelte'

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
  let baseTitle = $derived(editingToken ? t('Edit token') : cloningToken ? t('Clone token') : t('New token'))
  let subtitle = $derived(view === 'success' ? t('Token created successfully') : tokenName.trim() ? tokenName.trim() : `${t('Step')} ${wizardStep} ${t('of 3')}`)
  let stepperConfig: StepperConfig | null = $derived(view === 'success' ? null : { current: wizardStep, total: 3, labels: [t('Name & access'), t('Models'), t('Accounts')] })

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

  let hasAnyModelAvailable = $derived(providerModels.some((pm: ProviderModels) => (pm.models?.length ?? 0) > 0))
  let hasAnyCredentialAvailable = $derived(eligibleCredentialsGrouped.some((g: { creds: typeof allCredentials }) => g.creds.length > 0))

  let step1Valid = $derived(!!tokenName.trim() && (allowAllProviders || selectedProviders.size > 0))
  let step1Hint = $derived.by(() => {
    if (!tokenName.trim()) return t('Enter a token name.')
    if (!allowAllProviders && selectedProviders.size === 0) return t('Select at least one provider or enable Allow all.')
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
    if (selectedModels.size === 0) return t('Select at least one model or enable Allow all.')
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
    if (selectedCredentials.size === 0) return t('Select at least one account or enable Allow all.')
    return ''
  })
  let currentHint = $derived(view === 'success' ? '' : wizardStep === 1 ? step1Hint : wizardStep === 2 ? step2Hint : step3Hint)
  let currentValid = $derived(view === 'success' ? true : wizardStep === 1 ? step1Valid : wizardStep === 2 ? step2Valid : step3Valid)

  // grouped-checklist data (models + accounts share the same component)
  type Cred = (typeof allCredentials)[number]
  let modelGroups = $derived(
    providerModels.map((pm) => ({
      id: pm.provider_type,
      title: pm.provider_name,
      items: pm.models ?? [],
      error: pm.error
    }))
  )
  let accountGroups = $derived(
    eligibleCredentialsGrouped.map((g: (typeof eligibleCredentialsGrouped)[number]) => ({
      id: g.provider.id,
      title: g.provider.name,
      subtitle: `(${g.provider.id})`,
      items: g.creds as Cred[]
    }))
  )
  function modelItemId(group: any, item: string): string { return `${group.id}/${item}` }
  function credItemId(_group: any, item: Cred): string { return item.id }
  function credMatch(item: Cred, q: string): boolean {
    const lower = q.toLowerCase()
    return (item.label ?? '').toLowerCase().includes(lower) || item.id.toLowerCase().includes(lower)
  }
  function applyModelSelectAll(ids: string[], select: boolean): void {
    const s = new Set(selectedModels)
    for (const id of ids) { if (select) s.add(id); else s.delete(id) }
    selectedModels = s
  }
  function applyCredSelectAll(ids: string[], select: boolean): void {
    const s = new Set(selectedCredentials)
    for (const id of ids) { if (select) s.add(id); else s.delete(id) }
    selectedCredentials = s
  }

  function syncChrome(): void {
    untrack(() => {
      updateTitle(baseTitle)
      updateSubtitle(subtitle)
      updateStepper(stepperConfig)
      updateFooterHint(currentHint)
      if (view === 'success') {
        updateButtons([
          { label: t('Create another'), variant: 'secondary', onClick: resetWizard },
          { label: t('Done'), variant: 'primary', onClick: handleDone }
        ])
        return
      }
      if (wizardStep === 1) {
        updateButtons([
          { label: t('Cancel'), variant: 'secondary', onClick: closeModal },
          { label: t('Next'), variant: 'primary', onClick: goToStep2, disabled: !currentValid, loading: wizardLoading }
        ])
      } else if (wizardStep === 2) {
        updateButtons([
          { label: t('Back'), variant: 'secondary', onClick: goBackToStep1 },
          { label: t('Next'), variant: 'primary', onClick: goToStep3, disabled: !currentValid, loading: wizardLoading }
        ])
      } else {
        updateButtons([
          { label: t('Back'), variant: 'secondary', onClick: goBackToStep2 },
          { label: isEditMode ? t('Update token') : t('Create token'), variant: 'primary', onClick: submit, disabled: !currentValid, loading: wizardLoading }
        ])
      }
    })
  }

  function goBackToStep1(): void { wizardStep = 1; error = ''; syncChrome() }
  function goBackToStep2(): void { wizardStep = 2; error = ''; syncChrome() }
  function toggleProvider(id: string): void { selectedProviders = toggleSet(selectedProviders, id) }
  function toggleModel(fullId: string): void { selectedModels = toggleSet(selectedModels, fullId) }
  function toggleCredential(id: string): void { selectedCredentials = toggleSet(selectedCredentials, id) }

  async function goToStep2(): Promise<void> {
    error = ''
    if (!tokenName.trim()) { error = t('Token name is required.'); return }
    if (!allowAllProviders && selectedProviders.size === 0) { error = t('Select at least one provider or allow all.'); return }
    wizardLoading = true; syncChrome()
    try {
      const ids = allowAllProviders ? providers.map((p: Provider) => p.id) : [...selectedProviders]
      if (ids.length === 0) providerModels = []
      else {
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
      wizardStep = 2; error = ''
    } catch (e) { error = getErrorMessage(e) } finally { wizardLoading = false; syncChrome() }
  }

  async function goToStep3(): Promise<void> {
    error = ''
    if (!allowAllModels && !step2Valid) { error = step2Hint || t('Select at least one model or allow all.'); return }
    wizardLoading = true; syncChrome()
    try {
      if (allCredentials.length === 0) {
        try { const creds: any = await api.credentials.list(); allCredentials = Array.isArray(creds) ? creds : [] } catch (_) {}
      }
      const eligible = getEligibleProviderIds()
      for (const cid of [...selectedCredentials]) {
        const cred = allCredentials.find((c) => c.id === cid)
        if (!cred || !eligible.has(cred.provider_id)) selectedCredentials.delete(cid)
      }
      selectedCredentials = new Set(selectedCredentials)
      wizardStep = 3; error = ''
    } catch (e) { error = getErrorMessage(e) } finally { wizardLoading = false; syncChrome() }
  }

  async function submit(): Promise<void> {
    error = ''; wizardLoading = true; syncChrome()
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
        onComplete({}); closeModal()
      } else {
        const result: any = await api.tokens.create(payload as any)
        const tokenStr: string | null = result?.token ?? result?.Token ?? result?.token_hash ?? null
        createdToken = tokenStr
        if (tokenStr) { view = 'success'; error = '' }
        else { onComplete({ token: tokenStr ?? undefined }); closeModal(); return }
        syncChrome()
      }
    } catch (e) { error = getErrorMessage(e) } finally {
      wizardLoading = false
      if (view !== 'success') syncChrome()
      else { wizardLoading = false; syncChrome() }
    }
  }

  function handleDone(): void { onComplete({ token: createdToken ?? undefined }); closeModal() }
  function resetWizard(): void {
    tokenName = ''; allowAllProviders = false; selectedProviders = new Set(); providerModels = []
    allowAllModels = false; selectedModels = new Set(); allowAllCredentials = false; selectedCredentials = new Set()
    searchModels = ''; searchAccounts = ''; error = ''; createdToken = null; copied = false
    if (copyTimeout) clearTimeout(copyTimeout); wizardStep = 1; view = 'wizard'; syncChrome()
  }
  async function copyToken(): Promise<void> {
    if (!createdToken) return
    try { await navigator.clipboard.writeText(createdToken); copied = true; if (copyTimeout) clearTimeout(copyTimeout); copyTimeout = window.setTimeout(() => (copied = false), 2000) }
    catch (_) { error = t('Copy failed. Please select and copy manually.') }
  }

  $effect(() => {
    void wizardStep; void tokenName; void allowAllProviders; void selectedProviders.size; void allowAllModels; void selectedModels.size
    void allowAllCredentials; void selectedCredentials.size; void wizardLoading; void searchModels; void searchAccounts
    void view; void createdToken; void error; void subtitle; void stepperConfig; void currentHint
    untrack(() => syncChrome())
  })
</script>

{#if view === 'success'}
  <TokenSuccessView
    token={createdToken}
    scopeLabel={{
      providers: allowAllProviders ? t('All providers') : n(selectedProviders.size, 'provider', 'providers', 'провайдер', 'провайдера', 'провайдеров'),
      models: allowAllModels ? t('All models') : n(selectedModels.size, 'model', 'models', 'модель', 'модели', 'моделей'),
      accounts: allowAllCredentials ? t('All accounts') : n(selectedCredentials.size, 'account', 'accounts', 'аккаунт', 'аккаунта', 'аккаунтов')
    }}
    {copied}
    {error}
    onCopy={copyToken}
    onCreateAnother={resetWizard}
    onDone={handleDone}
  />
{:else if wizardStep === 1}
  {#if error}<div class="error-msg">{error}</div>{/if}
  <div class="form-group">
    <label for="token-name">{t('Token name')} *</label>
    <input id="token-name" type="text" bind:value={tokenName} placeholder={t('My Application')} />
  </div>
  <div class="form-group">
    <Switch bind:checked={allowAllProviders} label={t('Allow all providers')} id="allow-all-providers" />
    {#if allowAllProviders}<div class="hint">{t('All providers are allowed. The list below is disabled but visible.')}</div>{/if}
  </div>
  <div class="form-group" class:is-disabled={allowAllProviders}>
    <div class="form-label">{t('Providers')}</div>
    <ProviderTileGrid {providers} selected={selectedProviders} disabled={allowAllProviders} onToggle={toggleProvider} />
  </div>
{:else if wizardStep === 2}
  {#if error}<div class="error-msg">{error}</div>{/if}
  <div class="form-group">
    <Switch bind:checked={allowAllModels} label={t('Allow all models')} id="allow-all-models" />
    {#if allowAllModels}<div class="hint">{t('All models of the selected providers are allowed. The list below is disabled but visible.')}</div>{/if}
  </div>
  <SearchField bind:value={searchModels} placeholder={t('Search models...')} disabled={allowAllModels} />
  {#if providerModels.length === 0}
    <div class="muted-placeholder">{t('No providers selected — go back and select providers.')}</div>
  {:else}
    <GroupedChecklist
      groups={modelGroups}
      selected={selectedModels}
      query={searchModels}
      disabled={allowAllModels}
      emptyLabel={t('No models available for this provider')}
      noMatchLabel={t('No matches')}
      getItemId={modelItemId}
      onToggle={toggleModel}
      onSelectAll={applyModelSelectAll}
    >
      {#snippet row({ item, id, checked })}
        {@const traits = modelTraits(item)}
        <label class="checkbox-item">
          <input type="checkbox" {checked} onchange={() => toggleModel(id)} disabled={allowAllModels} />
          <span class="mono">{item}</span>
          {#each traits as t}<span class="badge {traitBadgeClass(t)} badge-sm">{t}</span>{/each}
        </label>
      {/snippet}
    </GroupedChecklist>
  {/if}
{:else if wizardStep === 3}
  {#if error}<div class="error-msg">{error}</div>{/if}
  <div class="form-group">
    <Switch bind:checked={allowAllCredentials} label={t('Allow all accounts')} id="allow-all-creds" />
    {#if allowAllCredentials}<div class="hint">{t('All accounts of the eligible providers are allowed. The list below is disabled but visible.')}</div>{/if}
  </div>
  <SearchField bind:value={searchAccounts} placeholder={t('Search accounts...')} disabled={allowAllCredentials} />
  {#if allCredentials.length === 0}
    <div class="muted-placeholder">{t('No accounts registered yet.')}</div>
  {:else if eligibleCredentialsGrouped.length === 0}
    <div class="muted-placeholder">{t('No accounts match the selected providers/models.')}</div>
  {:else}
    <GroupedChecklist
      groups={accountGroups}
      selected={selectedCredentials}
      query={searchAccounts}
      disabled={allowAllCredentials}
      emptyLabel={t('No accounts for this provider')}
      noMatchLabel={t('No matches')}
      getItemId={credItemId}
      matchItem={credMatch}
      onToggle={toggleCredential}
      onSelectAll={applyCredSelectAll}
    >
      {#snippet row({ item, id, checked })}
        <label class="checkbox-item cred-item">
          <input type="checkbox" {checked} onchange={() => toggleCredential(id)} disabled={allowAllCredentials} />
          <span class="cred-main">
            <span class="cred-label">{item.label || t('API Key')}</span>
            <span class="text-muted cred-meta">{formatRelativeTime(item.updated_at, 'short')} · <span class="mono">{item.id.slice(0, 8)}…</span></span>
          </span>
          {#if item.is_expired}<span class="badge badge-red badge-sm">{t('expired')}</span>{/if}
        </label>
      {/snippet}
    </GroupedChecklist>
  {/if}
{/if}

<style>
  .form-group { margin-bottom: 16px; }
  .form-label { font-size: 12px; font-weight: 500; color: var(--color-text-soft); margin-bottom: 6px; }
  /* .is-disabled, .hint, .text-muted, .muted-placeholder, .search-field, .badge-sm are now global in app.css */
  .checkbox-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 8px 16px; margin-top: 8px; }
  .cred-list { grid-template-columns: 1fr; }
  .checkbox-item { display: flex; align-items: center; gap: 8px; font-size: 14px; cursor: pointer; user-select: none; }
  .checkbox-item input[type='checkbox'] { width: auto; cursor: pointer; }
  .model-sections { display: flex; flex-direction: column; gap: 24px; margin-top: 16px; }
  .model-section { display: flex; flex-direction: column; gap: 8px; }
  .section-header { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .section-title { font-size: 13px; font-weight: 500; color: var(--color-text); }
  .section-count { font-size: 12px; color: var(--color-text-soft); }
  .select-all-btn { font-size: 12px; padding: 0 8px; height: 24px; }
  .cred-item { padding: 8px 12px; border: 1px solid var(--color-outline-soft); border-radius: 8px; background: var(--color-surface); }
  .cred-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
  .cred-label { font-size: 13px; font-weight: 500; color: var(--color-text); }
  .cred-meta { font-size: 11px; display: flex; gap: 6px; align-items: center; }
</style>
