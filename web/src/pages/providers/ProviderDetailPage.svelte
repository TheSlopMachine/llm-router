<script lang="ts">
  import { api } from '../../lib/api'
  import { modal } from '../../lib/modal.svelte'
  import { getErrorMessage } from '../../lib/errors'
  import { toast } from '../../lib/toast.svelte'
  import type { Credential, Provider, ProxyPool, TestResult, UINode } from '../../lib/types'
  import EditCredentialLabel from './components/EditCredentialLabel.svelte'
  import CustomProviderWizard from '../../components/wizards/CustomProviderWizard.svelte'
  import ProviderCredentialWizard from './components/ProviderCredentialWizard.svelte'
  import ModelsSection from './components/ModelsSection.svelte'
  import DynamicForm from '../../components/domain/DynamicForm.svelte'
  import { Button, Chip, FloatingView, HStack, Image, Select, Spacer, Switch, Table, Text, VStack } from '../../components/ui'
  import type { TableColumn } from '../../components/ui'
  import { squircle } from '../../lib/squircle'
  import { t } from '../../lib/i18n.svelte'

  let { providerId } = $props<{ providerId: string }>()

  let provider = $state<Provider | null>(null)
  let loading = $state(true)
  let error = $state('')

  let credentials = $state<Credential[]>([])
  let credentialsLoading = $state(false)
  let credentialTestResults = $state<Record<string, TestResult | 'loading'>>({})
  const resultTimers = new Map<string, ReturnType<typeof setTimeout>>()
  let disableFailedCredentials = $state(false)
  let testingAllCreds = $state(false)
  let testAllCredsCancel = $state(false)
  let credentialsEnabled = $state(true)

  // Bumped on credential add so ModelsSection retries discovery: a key
  // added to a credential-less provider heals the catalog without clicks.
  let credRevision = $state(0)

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
    { key: 'name', title: t('Name'), width: '1fr', priority: 1 },
    { key: 'actions', title: t('Actions'), width: 'auto', align: 'right', priority: 1 },
  ]

  $effect(() => {
    void providerId
    void loadPage()
  })

  $effect(() => {
    if (!provider) return
    void loadCapabilities()
    void reloadCredentials()
    initProxyConfig()
    void loadPools()
    disableFailedCredentials = provider.config?.disable_failed_credentials === true
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
      toast.success(enabled ? `${provider.name} enabled` : `${provider.name} disabled`)
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
      title: t('Edit Provider'),
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

  let deleteProviderAnchor = $state<HTMLElement>()
  let deletingProvider = $state(false)

  function openDeleteProvider(anchorEl?: HTMLElement): void {
    if (deleteProviderAnchor) {
      deleteProviderAnchor = undefined
    } else {
      deleteProviderAnchor = anchorEl
    }
  }

  async function confirmDeleteProvider(): Promise<void> {
    if (!provider || deletingProvider) return
    deletingProvider = true
    try {
      await api.providers.delete(provider.id)
      deleteProviderAnchor = undefined
      back()
    } catch (e) {
      error = getErrorMessage(e)
      deleteProviderAnchor = undefined
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
      title: `Add Credential · ${provider.name}`,
      content: ProviderCredentialWizard,
      severity: 'medium',
      size: 'medium',
      props: {
        provider,
        onComplete: async () => {
          modal.close()
          await reloadCredentials()
          credRevision += 1
        },
      },
    })
  }

  function openEditCredential(cred: Credential): void {
    modal.open({
      title: t('Edit credential'),
      content: EditCredentialLabel,
      severity: 'medium',
      size: 'small',
      props: {
        cred,
        onComplete: async () => {
          modal.close()
          await reloadCredentials()
        },
      },
    })
  }

  let deleteCredTarget = $state<Credential | null>(null)
  let deleteCredAnchor = $state<HTMLElement>()
  let deletingCred = $state(false)

  function openDeleteCredential(cred: Credential, anchorEl?: HTMLElement): void {
    if (deleteCredTarget?.id === cred.id) {
      deleteCredTarget = null
    } else {
      deleteCredTarget = cred
      deleteCredAnchor = anchorEl
    }
  }

  async function confirmDeleteCredential(): Promise<void> {
    const cred = deleteCredTarget
    if (!cred || deletingCred) return
    deletingCred = true
    try {
      await api.credentials.delete(cred.id)
      credentials = credentials.filter((c) => c.id !== cred.id)
      deleteCredTarget = null
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      deletingCred = false
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

  function isFailingCredential(res: TestResult): boolean {
    // Health verdicts only: the credential test runs check_health, so no
    // traffic codes can surface. Unknown stays enabled.
    return res.code === 'unhealthy'
  }

  async function probeCredential(cred: Credential): Promise<TestResult> {
    credentialTestResults = { ...credentialTestResults, [cred.id]: 'loading' }
    let res: TestResult
    try {
      res = await api.credentials.test(cred.id)
    } catch (e) {
      res = { ok: false, latency_ms: 0, error: getErrorMessage(e) }
    }
    credentialTestResults = { ...credentialTestResults, [cred.id]: res }
    return res
  }

  async function unparkCredential(cred: Credential): Promise<void> {
    try {
      await api.credentials.unpark(cred.id)
      toast.success(`"${cred.label || t('Unnamed')}" ${t('unparked')}`)
      await reloadCredentials()
    } catch (e) {
      toast.error(`${t('Unpark failed')}: ${getErrorMessage(e)}`)
    }
  }

  async function testCredential(cred: Credential): Promise<void> {
    const res = await probeCredential(cred)
    if (res.ok) toast.success(`"${cred.label || t('Unnamed')}" ${t('is healthy')} · ${res.latency_ms}ms`)
    else toast.error(`"${cred.label || t('Unnamed')}" ${t('failed')}: ${res.error}`)
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
        const res = await probeCredential(cred)
        if (!res.ok && isFailingCredential(res)) failed.push(cred)
      }
      if (!testAllCredsCancel && disableFailedCredentials) {
        for (const cred of failed) {
          await api.credentials.update(cred.id, { disabled: true })
        }
        if (failed.length > 0) {
          toast.success(`${failed.length} ${t('failing credentials disabled')}`)
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
    try {
      const [credSchema, settingsSchema, proxySchema] = await Promise.all([
        api.providers.credentialSchema(provider.id).catch(() => null),
        api.providers.settingsSchema(provider.id).catch(() => null),
        api.providers.proxySchema(provider.id).catch(() => null),
      ])
      credentialsEnabled = (credSchema?.enabled ?? false) || (credSchema?.nodes?.length ?? 0) > 0
      settingsNodes = settingsSchema?.nodes ?? []
      settingsValues = { ...((provider.config?.settings ?? {}) as Record<string, unknown>) }
      proxyNodes = proxySchema?.nodes ?? []
      proxiesEnabled = (proxySchema?.enabled ?? false) || proxyNodes.length > 0
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function testIcon(res: TestResult | 'loading' | undefined, idleTitle: string): { icon: string; title: string } {
    if (!res) return { icon: 'network_check', title: idleTitle }
    if (res === 'loading') return { icon: 'progress_activity', title: t('Testing…') }
    if (res.ok) return { icon: 'check_circle', title: `OK · ${res.latency_ms}ms` }
    return { icon: 'error', title: res.error ?? t('Failed') }
  }

  function initProxyConfig(): void {
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
      toast.success(t('Settings saved'))
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      savingSettings = false
    }
  }
</script>

{#if loading}
  <VStack align="center" gap={2} class="provider-state">
    <Text tone="soft">{t('Loading…')}</Text>
  </VStack>
{:else if !provider}
  <VStack align="center" gap={4} class="provider-state">
    <Text tone="soft">{t('Provider not found.')}</Text>
    <Button onclick={back}>{t('Back to providers')}</Button>
  </VStack>
{:else}
  <VStack gap={6} class="provider-detail">
    <HStack gap={4} align="center" class="detail-header">
      <Button onclick={back} ariaLabel={t('Back to providers')} title={t('Back to providers')} icon={{ name: 'arrow_back' }} />
      <Image src={provider.icon_url} alt={provider.name} width={40} height={40} radius={3} fit="contain" fallbackIcon="cloud" />
      <VStack gap={0} grow class={provider.disabled ? 'provider-off' : ''}>
        <Text tag="h1" size="lg" weight="bold">{provider.name}</Text>
        <Text tone="soft" size="sm">{provider.type_key}</Text>
      </VStack>
      <Spacer />
      {#if !provider.is_ui_readonly}
        <HStack gap={2}>
          <Button onclick={openEditProvider} icon={{ name: 'edit' }}>{t('Edit')}</Button>
          <Button tint="#dc2626" onclick={(e) => openDeleteProvider(e.currentTarget as HTMLElement)} icon={{ name: 'delete' }}>{t('Delete')}</Button>
        </HStack>
      {/if}
      <Switch
        checked={!provider.disabled}
        ariaLabel={t('Enable provider')}
        size="xl"
        onchange={(v) => toggleProviderEnabled(v)}
      />
    </HStack>

    {#if error}
      <VStack class="error-msg" gap={0}>
        <Text tone="danger" size="sm">{error}</Text>
      </VStack>
    {/if}

    {#if credentialsEnabled}
      <VStack tag="section" gap={4} class="provider-section">
        <HStack align="center" gap={2}>
          <Text tag="h2" size="md" weight="medium">{t('Credentials')}</Text>
          <Text size="xs" tone="soft">{credentials.length}</Text>
          <Spacer />
          <Button style="prominent" onclick={openAddCredential} icon={{ name: 'add' }}>{t('Add credential')}</Button>
        </HStack>
      <HStack align="center" gap={4} wrap>
        <Switch
          checked={disableFailedCredentials}
          label={t('Disable failing credentials')}
          onchange={(v) => { disableFailedCredentials = v; void saveCredAutomation() }}
        />
        <Button icon={{ name: testingAllCreds ? 'stop' : 'network_check' }} onclick={testAllCredentials}>
          {testingAllCreds ? t('Testing… click to cancel') : t('Test all')}
        </Button>
      </HStack>
      <Table
        columns={credentialColumns}
        rows={credentials}
        rowKey={(cred) => cred.id}
        loading={credentialsLoading}
        rowClass={(cred) => cred.disabled ? 'row-off' : ''}
      >
        {#snippet cell({ column, row })}
          {@const cred = row as Credential}
          {#if column.key === 'name'}
            <HStack gap={2} align="center" wrap>
              <Text size="base" weight="medium">{cred.label || t('Unnamed')}</Text>
              {#if cred.is_expired}
                <Chip text={t('Expired')} color="chip-red" />
              {/if}
            </HStack>
            {#if cred.disabled && (cred.disabled_by === 'system' || cred.disabled_by === 'healthcheck')}
              <Text size="sm" tone="danger">{t('Disabled automatically')}{cred.disabled_reason ? `: ${cred.disabled_reason}` : ''}</Text>
            {:else if cred.disabled && cred.disabled_by === 'plugin'}
              <Text size="sm" tone="danger">{t('Disabled by plugin')}{cred.disabled_reason ? `: ${cred.disabled_reason}` : ''}</Text>
            {/if}
          {:else}
            {@const ti = testIcon(credentialTestResults[cred.id], t('Test credential'))}
            <HStack gap={3} justify="end">
              {#if cred.parked}
                <Button
                  size="small"
                  style="text"
                  icon={{ name: 'ac_unit' }}
                  ariaLabel={t('Unpark credential')}
                  title={`${t('Unpark credential')}${cred.park_reason ? `: ${cred.park_reason}` : ''}`}
                  onclick={() => unparkCredential(cred)}
                />
              {/if}
              <Button
                size="small"
                icon={{ name: ti.icon }}
                style="text"
                disabled={credentialTestResults[cred.id] === 'loading'}
                ariaLabel={ti.title}
                title={ti.title}
                onclick={() => testCredential(cred)}
              />
              <Button size="small" style="text" icon={{ name: 'edit' }} ariaLabel={t('Edit credential')} title={t('Edit credential')} onclick={() => openEditCredential(cred)} />
              <Button size="small" tint="#dc2626" style="text" icon={{ name: 'delete' }} ariaLabel={t('Delete credential')} title={t('Delete credential')} onclick={(e) => openDeleteCredential(cred, e.currentTarget as HTMLElement)} />
              <Switch
                checked={!cred.disabled}
                ariaLabel={t('Enable credential')}
                onchange={(v) => toggleCredential(cred, v)}
              />
            </HStack>
          {/if}
        {/snippet}
        {#snippet empty()}
          <VStack align="center" gap={2} class="table-empty">
            <Text size="sm" tone="soft">{t('No credentials yet. Add one to route traffic to this provider.')}</Text>
          </VStack>
        {/snippet}
      </Table>
      </VStack>
    {/if}

    {#if proxiesEnabled}
      <VStack tag="section" gap={4} align="start" class="provider-section">
        <Text tag="h2" size="md" weight="medium">{t('Proxy')}</Text>
        <HStack align="center" gap={3} wrap>
          <Switch
            checked={proxyEnabled}
            label={t('Route through a proxy pool')}
            onchange={(v) => { proxyEnabled = v; void saveProxyConfig() }}
          />
          {#if proxyEnabled}
            <Select
              bind:value={proxyPool}
              options={[
                { value: 'auto', label: t('Auto pool') },
                ...poolOptions.map((p) => ({ value: p.id, label: p.name })),
              ]}
              ariaLabel={t('Proxy pool')}
              autoWidth
              onchange={() => void saveProxyConfig()}
            />
          {/if}
          {#if savingProxy}
            <Text size="xs" tone="soft">{t('Saving…')}</Text>
          {/if}
        </HStack>
        {#if proxyEnabled && proxyNodes.length > 0}
          <DynamicForm nodes={proxyNodes} bind:values={settingsValues} busy={savingProxy} />
        {/if}
      </VStack>
    {/if}

    {#if settingsNodes.length > 0}
      <VStack tag="section" gap={4} align="start" class="provider-section">
        <HStack align="center" gap={2}>
          <Text tag="h2" size="md" weight="medium">{t('Plugin settings')}</Text>
          <Spacer />
          <Button style="prominent" onclick={() => void saveSettings()} disabled={savingSettings}>
            {savingSettings ? t('Saving…') : t('Save settings')}
          </Button>
        </HStack>
        <DynamicForm nodes={settingsNodes} bind:values={settingsValues} busy={savingSettings} />
      </VStack>
    {/if}

    <ModelsSection bind:provider onrefresh={loadPage} credRevision={credRevision} />
  </VStack>
{/if}

<FloatingView
  open={Boolean(deleteCredTarget)}
  anchor={deleteCredAnchor}
  onclose={() => { deleteCredTarget = null }}
  label={t('Delete credential')}
>
  {#snippet children({ close })}
    <VStack gap={3} style="max-width: 280px;">
      <VStack gap={1}>
        <Text weight="medium" size="base">{t('Delete credential')}</Text>
        <Text size="sm" tone="soft">
          {t('Are you sure you want to delete')} "{deleteCredTarget?.label || t('Unnamed')}"? {t('This action cannot be undone.')}
        </Text>
      </VStack>
      <HStack justify="end" gap={2}>
        <Button size="small" style="text" onclick={close} disabled={deletingCred}>{t('Cancel')}</Button>
        <Button
          size="small"
          style="prominent"
          tint="#dc2626"
          disabled={deletingCred}
          onclick={confirmDeleteCredential}
        >
          {deletingCred ? t('Deleting…') : t('Delete')}
        </Button>
      </HStack>
    </VStack>
  {/snippet}
</FloatingView>

<FloatingView
  open={Boolean(deleteProviderAnchor)}
  anchor={deleteProviderAnchor}
  onclose={() => { deleteProviderAnchor = undefined }}
  label={t('Delete Provider')}
>
  {#snippet children({ close })}
    <VStack gap={3} style="max-width: 280px;">
      <VStack gap={1}>
        <Text weight="medium" size="base">{t('Delete Provider')}</Text>
        <Text size="sm" tone="soft">
          {t('Are you sure you want to delete')} "{provider?.name}"? {t('Credentials for this provider will be removed as well.')}
        </Text>
      </VStack>
      <HStack justify="end" gap={2}>
        <Button size="small" style="text" onclick={close} disabled={deletingProvider}>{t('Cancel')}</Button>
        <Button
          size="small"
          style="prominent"
          tint="#dc2626"
          disabled={deletingProvider}
          onclick={confirmDeleteProvider}
        >
          {deletingProvider ? t('Deleting…') : t('Delete')}
        </Button>
      </HStack>
    </VStack>
  {/snippet}
</FloatingView>

<style>
  :global(.provider-detail),
  :global(.provider-section) {
    width: 100%;
  }

  :global(.provider-state) {
    min-height: 240px;
    justify-content: center;
  }

  :global(.provider-off) {
    opacity: 0.55;
  }

  :global(.error-msg) {
    background: var(--color-notification-error-bg);
    padding: var(--space-4) var(--space-5);
    border-radius: var(--radius-md);
  }

  :global(.table-empty) {
    width: 100%;
    padding: var(--space-4);
  }

  @media (max-width: 768px) {
    :global(.detail-header) {
      flex-wrap: wrap;
    }
  }
</style>
