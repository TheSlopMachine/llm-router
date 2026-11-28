<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import { modal, toast, ContentUnavailable, ProgressView, Button, Chip, DeleteButton, HStack, Section, Toggle, Table, Text, VStack, Banner, Header, confirmAt, type TableColumn, Picker, type HeaderAction, type IconName } from '@fuckyouryan/fui'
  import { hasUiNodes } from '$lib/ui-helpers'
  import { getErrorMessage } from '../../lib/errors'
  import type { Credential, Provider, ProxyPool, TestResult, UINode } from '../../lib/types'
  import EditCredentialLabel from './components/EditCredentialLabel.svelte'
  import CustomProviderWizard from '../../components/wizards/CustomProviderWizard.svelte'
  import ProviderCredentialWizard from './components/ProviderCredentialWizard.svelte'
  import ModelsSection from './components/ModelsSection.svelte'
  import DockerSection from './components/DockerSection.svelte'
  import type { DockerSectionState } from '$lib/docker'
  import DynamicForm from '../../components/domain/DynamicForm.svelte'
  import { t, tx } from '$lib/i18n.svelte'
  import { isAutoDisabled, isPluginDisabled } from '$lib/credential-state'
  import { formatDisableReason } from '$lib/format'

  let { providerId }: { providerId: string } = $props()

  let provider = $state<Provider | null>(null)
  let loading = $state(true)
  let error = $state('')

  let credentials = $state<Credential[]>([])
  let credentialsLoading = $state(false)
  let credentialTestResults = $state<Record<string, TestResult | 'loading'>>({})
  const resultTimers = new Map<string, ReturnType<typeof setTimeout>>()
  // Writable derived: a toggle write overrides until the provider changes.
  let disableFailedCredentials = $derived(provider?.config?.disable_failed_credentials === true)
  let testingAllCreds = $state(false)
  let testAllCredsCancel = $state(false)
  let credentialsEnabled = $state(true)

  // Discovery retries on credential add (a key added to a credential-less provider heals the
  // catalog without clicks) and when the plugin's containers finish preparing.
  let modelsSection = $state<ModelsSection>()

  // One line on the plugin's containers while they prepare, '' otherwise.
  let dockerWait = $state('')
  let dockerWasBusy = false

  function onDockerState(state: DockerSectionState): void {
    dockerWait = state.busy ? state.waitText : ''
    if (dockerWasBusy && !state.busy) modelsSection?.retryDiscovery()
    dockerWasBusy = state.busy
  }

  let proxyEnabled = $state(false)
  let proxyPool = $state('auto')
  let poolOptions = $state<ProxyPool[]>([])
  let proxyNodes = $state<UINode[]>([])
  let proxiesEnabled = $state(false)
  let savingProxy = $state(false)

  let settingsNodes = $state<UINode[]>([])
  let settingsValues = $state<Record<string, unknown>>({})
  let savingSettings = $state(false)

  const providerIdValue = $derived(provider?.id ?? '')


  const credentialColumns: TableColumn[] = [
    { key: 'name', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'actions', title: t('common.labels.actions'), width: 'auto', align: 'right', priority: 1 },
  ]

  // The dashboard mounts this page once per provider id (its Swap key), so everything loads once
  // here. A refetch after a toggle replaces the provider object and keeps unsaved form edits.
  onMount(async () => {
    await loadPage()
    if (!provider) return
    void loadCapabilities()
    void reloadCredentials()
    initializeProxyConfig()
    void loadPools()
  })

  async function loadPage(): Promise<void> {
    loading = true
    error = ''
    try {
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? null
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function toggleProviderEnabled(enabled: boolean): Promise<void> {
    if (!provider) return
    error = ''
    try {
      await api.providers.updateInstance(provider.id, { name: provider.name, disabled: !enabled })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? provider
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function back(): void {
    window.location.hash = '#/providers'
  }

  function openEditProvider(): void {
    if (!provider) return
    modal.open({
      title: t('providers.detail.edit'),
      content: CustomProviderWizard,
      severity: 'medium',
      size: 'md',
      props: {
        editingProvider: provider,
        onComplete: async () => {
          modal.close()
          await loadPage()
        },
      },
    })
  }

  function detailActions(p: Provider): HeaderAction[] {
    if (p.is_ui_readonly) return []
    return [
      { id: 'edit', text: t('common.actions.edit'), icon: 'edit', onclick: openEditProvider },
      { id: 'delete', text: t('common.actions.delete'), icon: 'delete', tint: 'danger', onclick: () => void confirmDeleteProvider() },
    ]
  }

  function detailEnable(p: Provider): HeaderAction {
    return { id: 'enable', text: p.disabled ? t('common.labels.disabled') : t('common.labels.enabled'), icon: 'power_settings_new', iconOnly: true, selected: !p.disabled, onclick: () => void toggleProviderEnabled(Boolean(p.disabled)) }
  }

  let deletingProvider = $state(false)

  async function confirmDeleteProvider(): Promise<void> {
    if (!provider || deletingProvider) return
    const confirmed = await modal.confirm({
      title: t('providers.detail.delete'),
      message: `${t('misc.delete_confirm')} "${provider.name}"? ${t('providers.detail.credentials_removed')}`,
      confirmText: t('common.actions.delete'),
      confirmRole: 'destructive',
    })
    if (!confirmed) return
    deletingProvider = true
    try {
      await api.providers.delete(provider.id)
      back()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      deletingProvider = false
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

  async function reloadCredentials(): Promise<void> {
    if (!providerIdValue) return
    credentialsLoading = true
    try {
      const creds = await api.credentials.list()
      credentials = sortCredentials((creds as Credential[]).filter((c) => c.provider_id === providerIdValue))
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      credentialsLoading = false
    }
  }

  function openAddCredential(): void {
    if (!provider) return
    modal.open({
      title: tx('credentials.add_title', { name: provider.name }),
      content: ProviderCredentialWizard,
      severity: 'medium',
      size: 'md',
      props: {
        provider,
        onComplete: async () => {
          modal.close()
          await reloadCredentials()
          modelsSection?.retryDiscovery()
        },
      },
    })
  }

  function openEditCredential(cred: Credential): void {
    modal.open({
      title: t('credentials.edit'),
      content: EditCredentialLabel,
      severity: 'medium',
      size: 'sm',
      props: {
        cred,
        onComplete: async () => {
          modal.close()
          await reloadCredentials()
        },
      },
    })
  }

  async function deleteCredential(cred: Credential, anchor: HTMLElement): Promise<void> {
    error = ''
    try {
      const deleted = await confirmAt(anchor, {
        title: t('credentials.delete'),
        body: `${t('misc.delete_confirm')} "${cred.label || t('common.labels.unnamed')}"? ${t('common.undo.cannot_undo')}`,
        busyLabel: t('common.actions.deleting'),
        run: () => api.credentials.delete(cred.id),
      })
      if (deleted) credentials = credentials.filter((c) => c.id !== cred.id)
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

  function flashTimer(key: string, clear: () => void): void {
    const old = resultTimers.get(key)
    if (old) clearTimeout(old)
    resultTimers.set(key, setTimeout(clear, 3000))
  }

  async function saveCredAutomation(): Promise<void> {
    if (!provider) return
    error = ''
    try {
      // Patch only the automation field on top of whatever config the
      // server currently has -- never reconstruct the proxy/geo portion here.
      await api.providers.updateInstance(provider.id, {
        name: provider.name,
        config: {
          ...(provider.config ?? {}),
          disable_failed_credentials: disableFailedCredentials,
        },
      })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerIdValue) ?? provider
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function isFailingCredential(result: TestResult): boolean {
    // Health verdicts only: the credential test runs check_health, so no
    // traffic codes can surface. Unknown stays enabled.
    return result.code === 'unhealthy'
  }

  async function probeCredential(cred: Credential): Promise<TestResult> {
    credentialTestResults = { ...credentialTestResults, [cred.id]: 'loading' }
    let result: TestResult
    try {
      result = await api.credentials.test(cred.id)
    } catch (e) {
      result = { ok: false, latency_ms: 0, error: getErrorMessage(e) }
    }
    credentialTestResults = { ...credentialTestResults, [cred.id]: result }
    return result
  }

  async function unparkCredential(cred: Credential): Promise<void> {
    try {
      await api.credentials.unpark(cred.id)
      await reloadCredentials()
    } catch (e) {
      toast.error(`${t('credentials.unpark_failed')}: ${getErrorMessage(e)}`)
    }
  }

  async function testCredential(cred: Credential): Promise<void> {
    const result = await probeCredential(cred)
    if (result.ok) toast.success(`"${cred.label || t('common.labels.unnamed')}" ${t('credentials.status.healthy')} · ${result.latency_ms}ms`)
    else toast.error(`"${cred.label || t('common.labels.unnamed')}" ${t('credentials.status.failed')}: ${result.error}`)
    flashTimer(`cred:${cred.id}`, () => {
      const next = { ...credentialTestResults }
      delete next[cred.id]
      credentialTestResults = next
    })
  }

  async function testAllCredentials(): Promise<void> {
    if (testingAllCreds) {
      testAllCredsCancel = true
      return
    }
    testingAllCreds = true
    testAllCredsCancel = false
    for (const key of [...resultTimers.keys()]) {
      if (key.startsWith('cred:')) {
        clearTimeout(resultTimers.get(key))
        resultTimers.delete(key)
      }
    }
    try {
      const failed: Credential[] = []
      for (const cred of credentials) {
        if (testAllCredsCancel) break
        if (cred.disabled) continue
        const result = await probeCredential(cred)
        if (!result.ok && isFailingCredential(result)) failed.push(cred)
      }
      if (!testAllCredsCancel && disableFailedCredentials) {
        for (const cred of failed) {
          await api.credentials.update(cred.id, { disabled: true })
        }
        if (failed.length > 0) {
          toast.success(`${failed.length} ${t('credentials.status.failing_disabled')}`)
          await reloadCredentials()
        }
      }
    } finally {
      testingAllCreds = false
      testAllCredsCancel = false
    }
  }

  async function loadCapabilities(): Promise<void> {
    if (!provider) return
    // A schema that fails to load hides its section and says why; the other sections still load.
    const reportFailure = (failure: unknown): null => {
      error = getErrorMessage(failure)
      return null
    }
    const [credSchema, settingsSchema, proxySchema] = await Promise.all([
      api.providers.credentialSchema(provider.id).catch(reportFailure),
      api.providers.settingsSchema(provider.id).catch(reportFailure),
      api.providers.proxySchema(provider.id).catch(reportFailure),
    ])
    credentialsEnabled = hasUiNodes(credSchema)
    settingsNodes = settingsSchema?.nodes ?? []
    settingsValues = { ...((provider.config?.settings ?? {}) as Record<string, unknown>) }
    proxyNodes = proxySchema?.nodes ?? []
    proxiesEnabled = hasUiNodes(proxySchema)
  }

  function testIcon(result: TestResult | 'loading' | undefined, idleTitle: string): { icon: IconName; title: string } {
    if (!result) return { icon: 'network_check', title: idleTitle }
    if (result === 'loading') return { icon: 'progress_activity', title: t('credentials.testing') }
    if (result.ok) return { icon: 'check_circle', title: `OK · ${result.latency_ms}ms` }
    return { icon: 'error', title: result.error ?? t('common.state.failed') }
  }

  function initializeProxyConfig(): void {
    const raw = (provider?.config?.proxy ?? null) as { pool?: string } | null
    const pool = typeof raw?.pool === 'string' ? raw.pool : ''
    proxyEnabled = pool !== ''
    proxyPool = pool || 'auto'
  }

  async function loadPools(): Promise<void> {
    try {
      poolOptions = await api.proxies.pools.list()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function saveProxyConfig(): Promise<void> {
    if (!provider) return
    savingProxy = true
    error = ''
    try {
      const next = { ...(provider.config ?? {}) } as Record<string, unknown>
      if (proxyEnabled) {
        next.proxy = { pool: proxyPool || 'auto' }
      } else {
        delete next.proxy
      }
      await api.providers.updateInstance(provider.id, { name: provider.name, config: next })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? provider
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      savingProxy = false
    }
  }

  async function saveSettings(): Promise<void> {
    if (!provider) return
    savingSettings = true
    error = ''
    try {
      await api.providers.updateInstance(provider.id, {
        name: provider.name,
        config: { ...(provider.config ?? {}), settings: settingsValues },
      })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? provider
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      savingSettings = false
    }
  }
</script>

{#snippet credentialState(cred: Credential)}
  {#if isAutoDisabled(cred)}
    <Text variant="caption" tint="danger">{t('providers.detail.disabled_auto')}{formatDisableReason(cred.disabled_reason)}</Text>
  {:else if isPluginDisabled(cred)}
    <Text variant="caption" tint="danger">{t('providers.detail.disabled_by_plugin')}{formatDisableReason(cred.disabled_reason)}</Text>
  {/if}
{/snippet}

{#snippet credentialActions(cred: Credential)}
  <Button buttonStyle="text" icon={{ name: 'edit' }} ariaLabel={t('credentials.edit')} title={t('credentials.edit')} onclick={() => openEditCredential(cred)} />
  <DeleteButton label={t('credentials.delete')} onclick={(e) => void deleteCredential(cred, e.currentTarget as HTMLElement)} />
  <Toggle checked={!cred.disabled} ariaLabel={t('common.labels.enabled')} onchange={(v) => toggleCredential(cred, v)} />
{/snippet}

{#if loading}
  <ProgressView />
{:else if !provider}
  {#snippet backAction()}
    <Button buttonStyle="text" onclick={back}>{t('providers.detail.back_to_providers')}</Button>
  {/snippet}
  <ContentUnavailable title={t('providers.detail.not_found')} action={backAction} />
{:else}
  {@const p = provider}
  <Header title={p.name} subtitle={p.type_key} back={{ label: t('providers.list.title'), href: '#/providers' }} actions={detailActions(p)} primary={detailEnable(p)} />

  {#if error}
    <Banner tint="danger" text={error} />
  {/if}

  {#if credentialsEnabled}
    {#snippet addCredentialAction()}
      <Button buttonStyle="prominent" onclick={openAddCredential} icon={{ name: 'add' }}>{t('credentials.add')}</Button>
    {/snippet}
    <Section title={t('credentials.title_plural')} count={credentials.length} actions={addCredentialAction}>
      <HStack valign="center" wrap>
        <Toggle
          checked={disableFailedCredentials}
          label={t('credentials.disable_failing')}
          onchange={(v) => { disableFailedCredentials = v; void saveCredAutomation().then(() => { if (v && !testingAllCreds) void testAllCredentials() }) }}
        />
        <Button icon={{ name: testingAllCreds ? 'stop' : 'network_check' }} onclick={testAllCredentials}>
          {testingAllCreds ? t('credentials.testing_click_cancel') : t('credentials.test_all')}
        </Button>
      </HStack>
      <Table
        columns={credentialColumns}
        rows={credentials}
        rowKey={(cred) => cred.id}
        loading={credentialsLoading}
        rowClass={(cred) => cred.disabled ? 'row-off' : ''}
      >
        {#snippet card({ row })}
          {@const cred = row as Credential}
          <HStack valign="center">
            <VStack spacing="tight" grow>
              <Text variant="value">{cred.label || t('common.labels.unnamed')}</Text>
              {@render credentialState(cred)}
              {#if cred.is_expired}
                <Text variant="caption" tint="danger">{t('tokens.status.expired')}</Text>
              {/if}
            </VStack>
            <HStack>{@render credentialActions(cred)}</HStack>
          </HStack>
        {/snippet}
        {#snippet cell({ column, row })}
          {@const cred = row as Credential}
          {#if column.key === 'name'}
            <HStack valign="center" wrap>
              <Text variant="value">{cred.label || t('common.labels.unnamed')}</Text>
              {#if cred.is_expired}
                <Chip text={t('tokens.status.expired')} tint="danger" />
              {/if}
            </HStack>
            {@render credentialState(cred)}
          {:else}
            {@const ti = testIcon(credentialTestResults[cred.id], t('credentials.test'))}
            <HStack halign="end">
              {#if cred.parked}
                <Button
                  buttonStyle="text"
                  icon={{ name: 'ac_unit' }}
                  ariaLabel={t('credentials.unpark_action')}
                  title={`${t('credentials.unpark_action')}${cred.park_reason ? `: ${cred.park_reason}` : ''}`}
                  onclick={() => unparkCredential(cred)}
                />
              {/if}
              <Button
                icon={{ name: ti.icon }}
                buttonStyle="text"
                disabled={credentialTestResults[cred.id] === 'loading'}
                ariaLabel={ti.title}
                title={ti.title}
                onclick={() => testCredential(cred)}
              />
              {@render credentialActions(cred)}
            </HStack>
          {/if}
        {/snippet}
        {#snippet empty()}
          <ContentUnavailable title={t('credentials.empty')} />
        {/snippet}
      </Table>
    </Section>
  {/if}

  {#if proxiesEnabled}
    <Section title={t('proxy.title_singular')}>
        <HStack valign="center">
          <Text>{t('providers.detail.route_pool')}</Text>
          <Toggle
            checked={proxyEnabled}
            ariaLabel={t('providers.detail.route_pool')}
            onchange={(v) => { proxyEnabled = v; void saveProxyConfig() }}
          />
        </HStack>
        {#if proxyEnabled}
          <HStack valign="center" wrap>
            <Picker
              bind:value={proxyPool}
              options={[
                { value: 'auto', label: t('providers.detail.auto_pool') },
                ...poolOptions.map((p) => ({ value: p.id, label: p.name })),
              ]}
              ariaLabel={t('providers.detail.proxy_pool')}
              autoWidth
              onchange={() => void saveProxyConfig()}
            />
            {#if savingProxy}
              <Text size="xs" foregroundStyle="secondary">{t('common.actions.saving')}</Text>
            {/if}
          </HStack>
        {/if}
        {#if proxyEnabled && proxyNodes.length > 0}
          <DynamicForm nodes={proxyNodes} bind:values={settingsValues} busy={savingProxy} />
        {/if}
    </Section>
  {/if}

  {#if settingsNodes.length > 0}
    {#snippet saveSettingsAction()}
      <Button buttonStyle="prominent" onclick={() => void saveSettings()} disabled={savingSettings}>
        {savingSettings ? t('common.actions.saving') : t('providers.detail.save_settings')}
      </Button>
    {/snippet}
    <Section title={t('providers.detail.plugin_settings')} actions={saveSettingsAction}>
      <DynamicForm nodes={settingsNodes} bind:values={settingsValues} busy={savingSettings} />
    </Section>
  {/if}

  <DockerSection providerId={provider.id} onstate={onDockerState} />

  <ModelsSection bind:this={modelsSection} bind:provider={() => p, (next) => { provider = next }} waitingText={dockerWait} />
{/if}
