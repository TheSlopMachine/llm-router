<script lang="ts">
  import { onMount } from 'svelte'
  import { Banner, Button, Card, Chip, ContentUnavailable, DisclosureGroup, Field, HStack, Picker, Table, Text, TextField, VStack, modal, toast, type TableColumn } from '@fuckyouryan/fui'
  import DockerWizard from '../../components/domain/DockerWizard.svelte'
  import { api } from '$lib/api'
  import { t, tx } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { stageHint } from '$lib/docker'
  import type { DockerConnection, DockerConnectionTest, DockerRegistry, DockerStage } from '$lib/types'

  let connection = $state<DockerConnection | null>(null)
  let test = $state<DockerConnectionTest | null>(null)
  let testError = $state('')
  let testStage = $state<DockerStage | undefined>(undefined)
  let testing = $state(false)
  let error = $state('')

  let namespace = $state('')
  let dialMode = $state('auto')
  let gatewayImage = $state('')
  let savingAdvanced = $state(false)

  let registries = $state<DockerRegistry[]>([])
  let registryHost = $state('')
  let registryUsername = $state('')
  let registrySecret = $state('')
  let registrySaving = $state(false)

  let configured = $derived(!!connection?.endpoint)

  const registryColumns: TableColumn[] = [
    { key: 'host', title: t('settings.docker.registry_host.label'), width: '1fr', priority: 1 },
    { key: 'username', title: t('settings.docker.registry_username.label'), width: '1fr', priority: 2 },
    { key: 'actions', title: t('common.labels.actions'), width: 'auto', align: 'right', priority: 1 },
  ]

  onMount(async () => {
    try {
      const [stored, logins] = await Promise.all([api.docker.connection(), api.docker.registries()])
      applyConnection(stored)
      registries = logins
      if (stored.endpoint) await runTest()
    } catch (e) {
      error = getErrorMessage(e)
    }
  })

  function applyConnection(stored: DockerConnection): void {
    connection = stored
    namespace = stored.namespace
    dialMode = stored.dial_mode
    gatewayImage = stored.gateway_image
  }

  async function reload(): Promise<void> {
    applyConnection(await api.docker.connection())
  }

  async function runTest(): Promise<void> {
    testing = true
    testError = ''
    testStage = undefined
    test = null
    const answer = await api.docker.testConnection()
    if (answer.ok) {
      test = answer.result
    } else {
      testError = answer.error
      testStage = answer.stage
    }
    testing = false
  }

  function openWizard(): void {
    modal.open({
      title: t('settings.docker.wizard.title'),
      content: DockerWizard,
      size: 'md',
      severity: 'medium',
      props: { onComplete: async () => { await reload(); await runTest() } },
    })
  }

  function update(endpoint: string, clearSecrets: boolean) {
    const keep = clearSecrets ? '' : null
    return {
      endpoint,
      namespace,
      dial_mode: dialMode,
      gateway_image: gatewayImage,
      ssh_host_key: clearSecrets ? '' : connection?.ssh_host_key ?? '',
      wsl_distribution: clearSecrets ? '' : connection?.wsl_distribution ?? '',
      ssh_private_key: keep,
      ssh_passphrase: keep,
      tls_ca: keep,
      tls_certificate: keep,
      tls_key: keep,
    }
  }

  async function disconnect(): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('settings.docker.disconnect.action'),
      message: t('settings.docker.disconnect.hint'),
      confirmText: t('settings.docker.disconnect.action'),
      confirmRole: 'destructive',
    })
    if (!confirmed) return
    error = ''
    try {
      applyConnection(await api.docker.saveConnection(update('', true)))
      test = null
      testError = ''
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function saveAdvanced(): Promise<void> {
    savingAdvanced = true
    error = ''
    try {
      applyConnection(await api.docker.saveConnection(update(connection?.endpoint ?? '', false)))
      toast.success(t('settings.saved'))
      if (configured) await runTest()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      savingAdvanced = false
    }
  }

  async function saveRegistry(): Promise<void> {
    registrySaving = true
    error = ''
    try {
      await api.docker.saveRegistry(registryHost, registryUsername, registrySecret)
      registries = await api.docker.registries()
      registryHost = ''
      registryUsername = ''
      registrySecret = ''
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      registrySaving = false
    }
  }

  async function deleteRegistry(host: string): Promise<void> {
    error = ''
    try {
      await api.docker.deleteRegistry(host)
      registries = await api.docker.registries()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }
</script>

<Card title={t('settings.docker.title')} subtitle={t('settings.docker.desc')}>
  {#if error}
    <Banner tint="danger" text={error} />
  {/if}

  {#if !configured}
    <HStack gap={3} valign="center" wrap>
      <Text grow foregroundStyle="secondary">{t('settings.docker.unconfigured.status')}</Text>
      <Button buttonStyle="prominent" icon={{ name: 'add' }} onclick={openWizard}>{t('settings.docker.setup.action')}</Button>
    </HStack>
  {:else if connection}
    <VStack gap={2}>
      <HStack gap={2} valign="center" wrap>
        <Text weight="medium" grow truncate>{connection.endpoint}</Text>
        {#if testing}
          <Chip text={t('settings.docker.testing.status')} size="sm" tint="warning" />
        {:else if test}
          <Chip text={t('settings.docker.connected.status')} size="sm" tint="success" />
        {:else if testError}
          <Chip text={t('settings.docker.failed.status')} size="sm" tint="danger" />
        {/if}
      </HStack>
      {#if test}
        <Text size="sm" foregroundStyle="secondary">{tx('settings.docker.test.status', { version: test.server_version, mode: test.dial_mode })} · {test.operating_system} · {test.architecture}{test.gpu ? ` · ${t('settings.docker.gpu_available.status')}` : ''}</Text>
      {/if}
      {#if connection.wsl_distribution}
        <Text size="sm" foregroundStyle="secondary">{tx('settings.docker.wsl_keeper.status', { distribution: connection.wsl_distribution })}</Text>
      {/if}
      {#if connection.ssh_host_key_fingerprint}
        <Text size="sm" foregroundStyle="secondary">{connection.ssh_host_key_fingerprint}</Text>
      {/if}
      {#if testError}
        <Banner tint="danger" text={testError} />
        {#if testStage}
          <Text size="sm">{stageHint(testStage)}</Text>
        {/if}
      {/if}
    </VStack>
    <HStack halign="end" gap={2} wrap>
      <Button buttonStyle="text" tint="var(--fui-color-danger)" onclick={() => void disconnect()}>{t('settings.docker.disconnect.action')}</Button>
      <Button buttonStyle="neutral" onclick={() => void runTest()} disabled={testing}>{t('settings.docker.test.action')}</Button>
      <Button buttonStyle="neutral" icon={{ name: 'edit' }} onclick={openWizard}>{t('settings.docker.change.action')}</Button>
    </HStack>
  {/if}

  <DisclosureGroup title={t('settings.docker.advanced.title')}>
    <VStack gap={3}>
      <HStack gap={3} wrap>
        <Field label={t('settings.docker.dial_mode.label')}>
          <Picker
            bind:value={dialMode}
            options={[
              { value: 'auto', label: t('settings.docker.dial_mode.auto') },
              { value: 'route', label: t('settings.docker.dial_mode.route') },
              { value: 'relay', label: t('settings.docker.dial_mode.relay') },
            ]}
            ariaLabel={t('settings.docker.dial_mode.label')}
          />
        </Field>
        <Field label={t('settings.docker.namespace.label')}>
          <TextField bind:value={namespace} size="lg" autocomplete="off" />
        </Field>
        <Field label={t('settings.docker.gateway_image.label')}>
          <TextField bind:value={gatewayImage} placeholder={t('settings.docker.gateway_image.placeholder')} size="lg" autocomplete="off" />
        </Field>
      </HStack>
      <HStack halign="end">
        <Button buttonStyle="neutral" onclick={() => void saveAdvanced()} disabled={savingAdvanced}>{savingAdvanced ? t('common.actions.saving') : t('common.actions.save')}</Button>
      </HStack>
    </VStack>
  </DisclosureGroup>

  <DisclosureGroup title={t('settings.docker.registries.title')}>
    <VStack gap={2}>
      <Table columns={registryColumns} rows={registries} rowKey={(registry) => registry.host} hideHeaderWhenEmpty>
        {#snippet cell({ column, row })}
          {#if column.key === 'host'}
            <Text variant="value">{row.host}</Text>
          {:else if column.key === 'username'}
            <Text size="sm">{row.username}</Text>
          {:else}
            <HStack gap={3} halign="end">
              <Button buttonStyle="text" size="sm" tint="var(--fui-color-danger)" icon={{ name: 'delete' }} ariaLabel={t('common.actions.delete')} title={t('common.actions.delete')} onclick={() => void deleteRegistry(row.host)} />
            </HStack>
          {/if}
        {/snippet}
        {#snippet empty()}
          <ContentUnavailable title={t('settings.docker.registries.empty')} />
        {/snippet}
      </Table>
      <HStack gap={2} wrap valign="end">
        <Field label={t('settings.docker.registry_host.label')}>
          <TextField bind:value={registryHost} placeholder="ghcr.io" autocomplete="off" />
        </Field>
        <Field label={t('settings.docker.registry_username.label')}>
          <TextField bind:value={registryUsername} autocomplete="off" />
        </Field>
        <Field label={t('settings.docker.registry_secret.label')}>
          <TextField bind:value={registrySecret} secure autocomplete="off" />
        </Field>
        <Button buttonStyle="neutral" icon={{ name: 'add' }} onclick={() => void saveRegistry()} disabled={registrySaving || !registryHost || !registryUsername || !registrySecret}>{t('common.actions.add')}</Button>
      </HStack>
    </VStack>
  </DisclosureGroup>
</Card>
