<script lang="ts">
  import { onMount } from 'svelte'
  import { Banner, ChoiceList, Checkbox, CodeBlock, ContentUnavailable, Field, FilePicker, HStack, ModalChrome, Text, TextField, VStack, type ModalButton, type StepperConfig } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { t, tx } from '$lib/i18n.svelte'
  import type { TranslationKey } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { composeDockerEndpoint, parseDockerEndpoint, stageHint, DEFAULT_DOCKER_SOCKET, type DockerEndpointKind } from '$lib/docker'
  import type { DockerConnection, DockerConnectionTest, DockerStage } from '$lib/types'
  import DockerWizardSocket from './DockerWizardSocket.svelte'
  import DockerWizardWSL from './DockerWizardWSL.svelte'

  let {
    onComplete,
    closeModal,
  }: {
    onComplete: () => void
    closeModal: () => void
  } = $props()

  type Step = 'kind' | 'details' | 'host_key' | 'socket' | 'distribution' | 'service' | 'test'

  let stored = $state<DockerConnection | null>(null)
  let step = $state<Step>('kind')
  let kind = $state<DockerEndpointKind>('ssh')
  let host = $state('')
  let port = $state('22')
  let user = $state('')
  /** the daemon socket on the SSH host */
  let socket = $state(DEFAULT_DOCKER_SOCKET)
  /** the daemon socket on the router machine */
  let localSocket = $state('')
  let privateKey = $state('')
  let passphrase = $state('')
  let tlsCA = $state('')
  let tlsCertificate = $state('')
  let tlsKey = $state('')
  let hostKey = $state('')
  let fingerprint = $state('')
  let hostKeyConfirmed = $state(false)
  let busy = $state(false)
  let error = $state('')
  let stage = $state<DockerStage | undefined>(undefined)
  let result = $state<DockerConnectionTest | null>(null)
  let distribution = $state('')

  /** most used first: Docker on the router machine, then SSH, then TLS */
  const kinds: Array<{ value: DockerEndpointKind; title: TranslationKey; hint: TranslationKey }> = [
    { value: 'wsl', title: 'settings.docker.kind_wsl.label', hint: 'settings.docker.kind_wsl.hint' },
    { value: 'unix', title: 'settings.docker.kind_unix.label', hint: 'settings.docker.kind_unix.hint' },
    { value: 'ssh', title: 'settings.docker.kind_ssh.label', hint: 'settings.docker.kind_ssh.hint' },
    { value: 'tcp', title: 'settings.docker.kind_tcp.label', hint: 'settings.docker.kind_tcp.hint' },
  ]
  let availableKinds = $derived(kinds.filter((option) => stored?.connection_kinds.includes(option.value)))

  const stepLabelKeys: Record<Step, TranslationKey> = {
    kind: 'settings.docker.step_kind.label',
    details: 'settings.docker.step_details.label',
    host_key: 'settings.docker.step_host_key.label',
    socket: 'settings.docker.step_socket.label',
    distribution: 'settings.docker.step_distribution.label',
    service: 'settings.docker.step_service.label',
    test: 'settings.docker.step_test.label',
  }
  let endpoint = $derived(composeDockerEndpoint({ kind, host, port, user, socket: kind === 'unix' ? localSocket : socket }))
  const kindSteps: Record<DockerEndpointKind, Step[]> = {
    wsl: ['kind', 'distribution', 'service', 'test'],
    unix: ['kind', 'socket', 'test'],
    ssh: ['kind', 'details', 'host_key', 'test'],
    tcp: ['kind', 'details', 'test'],
  }
  let steps = $derived(kindSteps[kind])
  let stepLabels = $derived(steps.map((name) => t(stepLabelKeys[name])))

  let detailsValid = $derived.by(() => {
    switch (kind) {
      case 'ssh':
        return !!host.trim() && !!user.trim() && /^\d+$/.test(port.trim()) && (!!privateKey.trim() || !!stored?.has_ssh_private_key)
      case 'tcp':
        return !!host.trim() && /^\d+$/.test(port.trim())
      case 'unix':
      case 'wsl':
        return true
    }
  })

  let hostKeyCommand = $derived.by(() => {
    const type = hostKey.split(' ')[0] ?? ''
    if (type.includes('ed25519')) return 'ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub'
    if (type.startsWith('ecdsa')) return 'ssh-keygen -lf /etc/ssh/ssh_host_ecdsa_key.pub'
    return 'ssh-keygen -lf /etc/ssh/ssh_host_rsa_key.pub'
  })

  onMount(async () => {
    try {
      const connection = await api.docker.connection()
      stored = connection
      if (connection.endpoint) {
        const parts = parseDockerEndpoint(connection.endpoint, connection.connection_kinds)
        kind = parts.kind
        host = parts.host
        port = parts.port
        user = parts.user
        if (parts.kind === 'unix') localSocket = parts.socket
        else socket = parts.socket
        hostKey = connection.ssh_host_key
        fingerprint = connection.ssh_host_key_fingerprint
        distribution = connection.wsl_distribution
      } else if (availableKinds.length) {
        chooseKind(availableKinds[0].value)
      }
    } catch (e) {
      error = getErrorMessage(e)
    }
  })

  async function readFile(file: File, assign: (text: string) => void): Promise<void> {
    assign(await file.text())
  }

  function chooseKind(next: DockerEndpointKind): void {
    if (next === kind) return
    kind = next
    port = next === 'ssh' ? '22' : next === 'tcp' ? '2376' : port
  }

  function goTo(next: Step): void {
    error = ''
    stage = undefined
    step = next
    if (next === 'host_key') void scanHostKey()
    if (next === 'test') void saveAndTest()
  }

  function nextStep(): void {
    const index = steps.indexOf(step)
    if (index < steps.length - 1) goTo(steps[index + 1])
  }

  function previousStep(): void {
    const index = steps.indexOf(step)
    if (index > 0) goTo(steps[index - 1])
  }

  async function scanHostKey(): Promise<void> {
    busy = true
    hostKey = ''
    fingerprint = ''
    hostKeyConfirmed = false
    try {
      const scan = await api.docker.scanHostKey(endpoint)
      hostKeyConfirmed = scan.host_key === stored?.ssh_host_key && endpoint === stored?.endpoint
      hostKey = scan.host_key
      fingerprint = scan.fingerprint
    } catch (e) {
      error = getErrorMessage(e)
      hostKey = ''
      fingerprint = ''
    } finally {
      busy = false
    }
  }

  function secret(value: string): string | null {
    return value.trim() ? value : null
  }

  async function saveAndTest(): Promise<void> {
    busy = true
    result = null
    try {
      await api.docker.saveConnection({
        endpoint,
        namespace: stored?.namespace ?? '',
        dial_mode: stored?.dial_mode ?? 'auto',
        gateway_image: stored?.gateway_image ?? '',
        ssh_host_key: kind === 'ssh' ? hostKey : '',
        wsl_distribution: kind === 'wsl' ? distribution : '',
        ssh_private_key: kind === 'ssh' ? secret(privateKey) : null,
        ssh_passphrase: kind === 'ssh' ? secret(passphrase) : null,
        tls_ca: kind === 'tcp' ? secret(tlsCA) : null,
        tls_certificate: kind === 'tcp' ? secret(tlsCertificate) : null,
        tls_key: kind === 'tcp' ? secret(tlsKey) : null,
      })
      const answer = await api.docker.testConnection()
      if (answer.ok) {
        result = answer.result
      } else {
        error = answer.error
        stage = answer.stage
      }
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      busy = false
    }
  }

  function finish(): void {
    onComplete()
    closeModal()
  }

  const stepper = $derived<StepperConfig>({ current: steps.indexOf(step) + 1, total: steps.length, labels: stepLabels })

  /** undefined on the socket, distribution and service steps: DockerWizardSocket and DockerWizardWSL own those buttons */
  const buttons = $derived.by((): ModalButton[] | undefined => {
    const back: ModalButton = { label: t('common.actions.back'), buttonStyle: 'neutral', onclick: previousStep, disabled: busy }
    const next = (disabled: boolean): ModalButton => ({ label: t('common.actions.next'), buttonStyle: 'prominent', onclick: nextStep, disabled })
    switch (step) {
      case 'kind':
        return [{ label: t('common.actions.cancel'), buttonStyle: 'neutral', onclick: closeModal }, next(false)]
      case 'details':
        return [back, next(!detailsValid)]
      case 'host_key':
        return [back, next(busy || !hostKey || !hostKeyConfirmed)]
      case 'test':
        return result
          ? [{ label: t('common.actions.done'), buttonStyle: 'prominent', onclick: finish }]
          : [back, { label: t('settings.docker.retry.action'), buttonStyle: 'prominent', onclick: saveAndTest, disabled: busy, loading: busy }]
      default:
        return undefined
    }
  })
</script>

<ModalChrome title={t('settings.docker.wizard.title')} subtitle={t(stepLabelKeys[step])} {stepper} {buttons} />

<VStack gap={4}>
  {#if step === 'kind'}
    <ChoiceList options={availableKinds.map((option) => ({ value: option.value, title: t(option.title), hint: t(option.hint) }))} value={kind} ariaLabel={t('settings.docker.step_kind.label')} onchange={(next) => chooseKind(next as DockerEndpointKind)} />
  {:else if step === 'details'}
    {#if kind === 'ssh'}
      <HStack gap={3} wrap>
        <Field label={t('settings.docker.host.label')}>
          <TextField bind:value={host} placeholder="192.168.1.10" size="lg" autocomplete="off" />
        </Field>
        <Field label={t('settings.docker.port.label')}>
          <TextField bind:value={port} size="lg" autocomplete="off" />
        </Field>
        <Field label={t('settings.docker.user.label')}>
          <TextField bind:value={user} size="lg" autocomplete="off" />
        </Field>
      </HStack>
      <Field label={t('settings.docker.ssh_key.label')} hint={stored?.has_ssh_private_key ? t('settings.docker.secret.hint') : t('settings.docker.ssh_key.hint')}>
        <VStack gap={2}>
          <TextField bind:value={privateKey} minRows={3} maxRows={8} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" autocomplete="off" />
          <FilePicker buttonText={t('settings.docker.key_file.action')} icon="upload_file" buttonStyle="neutral" onpick={(files) => void readFile(files[0], (text) => { privateKey = text })} />
        </VStack>
      </Field>
      <Field label={t('settings.docker.ssh_passphrase.label')} hint={stored?.has_ssh_passphrase ? t('settings.docker.secret.hint') : ''}>
        <TextField bind:value={passphrase} secure size="lg" />
      </Field>
      <Field label={t('settings.docker.socket.label')}>
        <TextField bind:value={socket} size="lg" autocomplete="off" />
      </Field>
    {:else if kind === 'tcp'}
      <HStack gap={3} wrap>
        <Field label={t('settings.docker.host.label')}>
          <TextField bind:value={host} size="lg" autocomplete="off" />
        </Field>
        <Field label={t('settings.docker.port.label')}>
          <TextField bind:value={port} size="lg" autocomplete="off" />
        </Field>
      </HStack>
      {#each [
        { label: 'settings.docker.tls_ca.label', has: stored?.has_tls_ca, value: tlsCA, assign: (text: string) => { tlsCA = text } },
        { label: 'settings.docker.tls_certificate.label', has: stored?.has_tls_certificate, value: tlsCertificate, assign: (text: string) => { tlsCertificate = text } },
        { label: 'settings.docker.tls_key.label', has: stored?.has_tls_key, value: tlsKey, assign: (text: string) => { tlsKey = text } },
      ] as material (material.label)}
        <Field label={t(material.label as TranslationKey)} hint={material.value ? t('settings.docker.file_loaded.hint') : material.has ? t('settings.docker.secret.hint') : ''}>
          <FilePicker buttonText={t('settings.docker.pem_file.action')} icon="upload_file" buttonStyle="neutral" onpick={(files) => void readFile(files[0], material.assign)} />
        </Field>
      {/each}
      <Text size="sm" foregroundStyle="secondary">{t('settings.docker.tls.hint')}</Text>
    {/if}
  {:else if step === 'socket'}
    <DockerWizardSocket bind:socket={localSocket} onBack={previousStep} onNext={nextStep} />
  {:else if step === 'distribution' || step === 'service'}
    {#key step}
      <DockerWizardWSL {step} bind:distribution onBack={previousStep} onNext={nextStep} />
    {/key}
  {:else if step === 'host_key'}
    {#if busy && !hostKey}
      <ContentUnavailable title={t('settings.docker.host_key_scanning.status')} />
    {:else if hostKey}
      <Text>{t('settings.docker.host_key.hint')}</Text>
      <CodeBlock text={fingerprint} />
      <Text size="sm" foregroundStyle="secondary">{t('settings.docker.host_key_command.hint')}</Text>
      <CodeBlock text={hostKeyCommand} />
      <Checkbox bind:checked={hostKeyConfirmed} label={t('settings.docker.host_key_confirm.label')} />
    {/if}
  {:else}
    {#if busy}
      <ContentUnavailable title={t('settings.docker.testing.status')} />
    {:else if result}
      <Banner tint="success" text={tx('settings.docker.test.status', { version: result.server_version, mode: result.dial_mode })} />
      <VStack gap={1}>
        <Text size="sm">{result.operating_system} · {result.architecture}</Text>
        <Text size="sm" foregroundStyle="secondary">{result.gpu ? t('settings.docker.gpu_available.status') : t('settings.docker.gpu_missing.status')}</Text>
      </VStack>
    {/if}
  {/if}

  {#if error}
    <Banner tint="danger" text={error} />
    {#if stage}
      <Text size="sm">{stageHint(stage)}</Text>
    {/if}
  {/if}
</VStack>
