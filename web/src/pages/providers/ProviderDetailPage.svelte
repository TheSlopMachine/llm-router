<script lang="ts">
  import { api } from '../../lib/api'
  import { modal } from '../../FUI/core/modal.svelte'
  import { getErrorMessage } from '../../lib/errors'
  import { toast } from '../../FUI/core/toast.svelte'
  import type { Credential, Provider, ProxyPool, TestResult, UINode } from '../../lib/types'
  import EditCredentialLabel from './components/EditCredentialLabel.svelte'
  import CustomProviderWizard from '../../components/wizards/CustomProviderWizard.svelte'
  import ProviderCredentialWizard from './components/ProviderCredentialWizard.svelte'
  import ModelsSection from './components/ModelsSection.svelte'
  import DynamicForm from '../../components/domain/DynamicForm.svelte'
  import EmptyState from '../../FUI/composite/EmptyState.svelte'
  import { Button, Chip, FloatingView, HStack, Image, Select, Spacer, Switch, Table, Text, VStack, Banner, ConfirmAction, Header, ToolbarItem } from '$ui'
  import type { TableColumn } from '$ui'
  import { squircle } from '../../FUI/core/squircle'
  import { t } from '$lib/i18n.svelte'
  import { isAutoDisabled, isPluginDisabled } from '$lib/credential-state'
  import { formatDisableReason } from '$lib/format'
  import { hasUiNodes } from '../../FUI/core/ui-guards'

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
    { key: 'name', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'actions', title: t('common.labels.actions'), width: 'auto', align: 'right', priority: 1 },
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
      title: t('providers.detail.edit'),
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
      title: t('credentials.edit'),
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
      toast.success(`"${cred.label || t('common.labels.unnamed')}" ${t('credentials.status.unparked')}`)
      await reloadCredentials()
    } catch (e) {
      toast.error(`${t('credentials.unpark_failed')}: ${getErrorMessage(e)}`)
    }
  }

  async function testCredential(cred: Credential): Promise<void> {
    const res = await probeCredential(cred)
    if (res.ok) toast.success(`"${cred.label || t('common.labels.unnamed')}" ${t('credentials.status.healthy')} · ${res.latency_ms}ms`)
    else toast.error(`"${cred.label || t('common.labels.unnamed')}" ${t('credentials.status.failed')}: ${res.error}`)
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
    try {
      const [credSchema, settingsSchema, proxySchema] = await Promise.all([
        api.providers.credentialSchema(provider.id).catch(() => null),
        api.providers.settingsSchema(provider.id).catch(() => null),
        api.providers.proxySchema(provider.id).catch(() => null),
      ])
      credentialsEnabled = hasUiNodes(credSchema)
      settingsNodes = settingsSchema?.nodes ?? []
      settingsValues = { ...((provider.config?.settings ?? {}) as Record<string, unknown>) }
      proxyNodes = proxySchema?.nodes ?? []
      proxiesEnabled = hasUiNodes(proxySchema)
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function testIcon(res: TestResult | 'loading' | undefined, idleTitle: string): { icon: string; title: string } {
    if (!res) return { icon: 'network_check', title: idleTitle }
    if (res === 'loading') return { icon: 'progress_activity', title: t('credentials.testing') }
    if (res.ok) return { icon: 'check_circle', title: `OK · ${res.latency_ms}ms` }
    return { icon: 'error', title: res.error ?? t('common.state.failed') }
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
      toast.success(t('providers.detail.settings_saved'))
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      savingSettings = false
    }
  }
</script>

{#if loading}
  <EmptyState title={t('common.state.loading')} />
{:else if !provider}
  <VStack align="center" gap={4} class="provider-state">
    <EmptyState title={t('providers.detail.not_found')} />
    <Button style="text" onclick={back}>{t('providers.detail.back_to_providers')}</Button>
  </VStack>
{:else}
  {@const p = provider}
  <VStack gap={6} class="provider-detail">
    <Header title={p.name} subtitle={p.type_key}>
      {#snippet actions()}
        {#if !p.is_ui_readonly}
          <ToolbarItem priority={2}><Button style="text" onclick={openEditProvider} icon={{ name: 'edit' }}>{t('common.actions.edit')}</Button></ToolbarItem>
          <ToolbarItem priority={3}><Button style="text" tint="var(--fui-color-danger)" onclick={(e) => openDeleteProvider(e.currentTarget as HTMLElement)} icon={{ name: 'delete' }}>{t('common.actions.delete')}</Button></ToolbarItem>
        {/if}
        <ToolbarItem primary label={t('providers.detail.enable')}>
          <Switch
            size="xl"
            checked={!p.disabled}
            ariaLabel={t('providers.detail.enable')}
            onchange={(v) => toggleProviderEnabled(v)}
          />
        </ToolbarItem>
      {/snippet}
    </Header>

    {#if error}
      <Banner variant="error" text={error} />
    {/if}

    {#if credentialsEnabled}
      <VStack tag="section" gap={4} class="provider-section">
        <Header level="section" title={t('credentials.title_plural')} subtitle={String(credentials.length)}>
          {#snippet actions()}
            <Button style="prominent" onclick={openAddCredential} icon={{ name: 'add' }}>{t('credentials.add')}</Button>
          {/snippet}
        </Header>
      <HStack align="center" gap={4} wrap>
        <Switch
          checked={disableFailedCredentials}
          label={t('credentials.disable_failing')}
          onchange={(v) => { disableFailedCredentials = v; void saveCredAutomation() }}
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
          <HStack gap={3} align="center">
            <VStack gap={1} grow>
              <Text variant="value">{cred.label || t('common.labels.unnamed')}</Text>
              {#if isAutoDisabled(cred)}
                <Text variant="caption" tone="danger">{t('providers.detail.disabled_auto')}{formatDisableReason(cred.disabled_reason)}</Text>
              {:else if isPluginDisabled(cred)}
                <Text variant="caption" tone="danger">{t('providers.detail.disabled_by_plugin')}{formatDisableReason(cred.disabled_reason)}</Text>
              {/if}
              {#if cred.is_expired}
                <Text variant="caption" tone="danger">{t('tokens.status.expired')}</Text>
              {/if}
            </VStack>
            <HStack gap={2}>
              <Button size="small" style="text" icon={{ name: 'edit' }} ariaLabel={t('credentials.edit')} title={t('credentials.edit')} onclick={() => openEditCredential(cred)} />
              <Button size="small" tint="var(--fui-color-danger)" style="text" icon={{ name: 'delete' }} ariaLabel={t('credentials.delete')} title={t('credentials.delete')} onclick={(e) => openDeleteCredential(cred, e.currentTarget as HTMLElement)} />
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <span
                role="presentation"
                onclick={(e) => e.stopPropagation()}
                onkeydown={(e) => e.stopPropagation()}
              >
                <Switch
                  checked={!cred.disabled}
                  ariaLabel={t('credentials.enable')}
                  onchange={(v) => toggleCredential(cred, v)}
                />
              </span>
            </HStack>
          </HStack>
        {/snippet}
        {#snippet cell({ column, row })}
          {@const cred = row as Credential}
          {#if column.key === 'name'}
            <HStack gap={2} align="center" wrap>
              <Text variant="value">{cred.label || t('common.labels.unnamed')}</Text>
              {#if cred.is_expired}
                <Chip text={t('tokens.status.expired')} color="chip-red" />
              {/if}
            </HStack>
            {#if isAutoDisabled(cred)}
              <Text variant="caption" tone="danger">{t('providers.detail.disabled_auto')}{formatDisableReason(cred.disabled_reason)}</Text>
            {:else if isPluginDisabled(cred)}
              <Text variant="caption" tone="danger">{t('providers.detail.disabled_by_plugin')}{formatDisableReason(cred.disabled_reason)}</Text>
            {/if}
          {:else}
            {@const ti = testIcon(credentialTestResults[cred.id], t('credentials.test'))}
            <HStack gap={3} justify="end">
              {#if cred.parked}
                <Button
                  size="small"
                  style="text"
                  icon={{ name: 'ac_unit' }}
                  ariaLabel={t('credentials.unpark_action')}
                  title={`${t('credentials.unpark_action')}${cred.park_reason ? `: ${cred.park_reason}` : ''}`}
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
              <Button size="small" style="text" icon={{ name: 'edit' }} ariaLabel={t('credentials.edit')} title={t('credentials.edit')} onclick={() => openEditCredential(cred)} />
              <Button size="small" tint="var(--fui-color-danger)" style="text" icon={{ name: 'delete' }} ariaLabel={t('credentials.delete')} title={t('credentials.delete')} onclick={(e) => openDeleteCredential(cred, e.currentTarget as HTMLElement)} />
              <Switch
                checked={!cred.disabled}
                ariaLabel={t('credentials.enable')}
                onchange={(v) => toggleCredential(cred, v)}
              />
            </HStack>
          {/if}
        {/snippet}
        {#snippet empty()}
          <EmptyState title={t('credentials.empty')} />
        {/snippet}
      </Table>
      </VStack>
    {/if}

    {#if proxiesEnabled}
      <VStack tag="section" gap={4} align="start" class="provider-section">
        <Text variant="section-title">{t('proxy.title_singular')}</Text>
        <HStack align="center" justify="between" gap={3}>
          <Text grow>{t('providers.detail.route_pool')}</Text>
          <Switch
            checked={proxyEnabled}
            ariaLabel={t('providers.detail.route_pool')}
            onchange={(v) => { proxyEnabled = v; void saveProxyConfig() }}
          />
        </HStack>
        {#if proxyEnabled}
          <HStack align="center" gap={3} wrap>
            <Select
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
              <Text size="xs" tone="soft">{t('common.actions.saving')}</Text>
            {/if}
          </HStack>
        {/if}
        {#if proxyEnabled && proxyNodes.length > 0}
          <DynamicForm nodes={proxyNodes} bind:values={settingsValues} busy={savingProxy} />
        {/if}
      </VStack>
    {/if}

    {#if settingsNodes.length > 0}
      <VStack tag="section" gap={4} align="start" class="provider-section">
        <Header level="section" title={t('providers.detail.plugin_settings')}>
          {#snippet actions()}
            <Button style="prominent" onclick={() => void saveSettings()} disabled={savingSettings}>
              {savingSettings ? t('common.actions.saving') : t('providers.detail.save_settings')}
            </Button>
          {/snippet}
        </Header>
        <DynamicForm nodes={settingsNodes} bind:values={settingsValues} busy={savingSettings} />
      </VStack>
    {/if}

    <ModelsSection bind:provider onrefresh={loadPage} credRevision={credRevision} />
  </VStack>
{/if}

<FloatingView
  open={Boolean(deleteCredTarget)}
  anchor={deleteCredAnchor}
  width="sm"
  onclose={() => { deleteCredTarget = null }}
  label={t('credentials.delete')}
>
  {#snippet children({ close })}
    <ConfirmAction
      title={t('credentials.delete')}
      body={`${t('misc.delete_confirm')} "${deleteCredTarget?.label || t('common.labels.unnamed')}"? ${t('common.undo.cannot_undo')}`}
      busy={deletingCred}
      busyLabel={t('common.actions.deleting')}
      onCancel={close}
      onConfirm={confirmDeleteCredential} />
  {/snippet}
</FloatingView>

<FloatingView
  open={Boolean(deleteProviderAnchor)}
  anchor={deleteProviderAnchor}
  width="sm"
  onclose={() => { deleteProviderAnchor = undefined }}
  label={t('providers.detail.delete')}
>
  {#snippet children({ close })}
    <ConfirmAction
      title={t('providers.detail.delete')}
      body={`${t('misc.delete_confirm')} "${provider?.name}"? ${t('providers.detail.credentials_removed')}`}
      busy={deletingProvider}
      busyLabel={t('common.actions.deleting')}
      onCancel={close}
      onConfirm={confirmDeleteProvider} />
  {/snippet}
</FloatingView>

<style>
  :global(.provider-detail),
  :global(.provider-section) {
    width: 100%;
  }

  :global(.provider-state) {
    min-height: var(--fui-provider-state-min-h);
    justify-content: center;
  }

  :global(.provider-off) {
    opacity: var(--fui-opacity-disabled);
  }

  :global(.table-empty) {
    width: 100%;
    padding: var(--fui-space-4);
  }
</style>
