<!-- The WSL steps of the Docker wizard. Only a Windows router offers them. -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { Banner, ChoiceList, Card, CodeBlock, ContentUnavailable, ModalChrome, ProgressView, Text, VStack, type ModalButton } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { t, tx } from '$lib/i18n.svelte'
  import type { TranslationKey } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import type { WSLDistribution, WSLDockerStatus } from '$lib/types'

  let {
    step,
    distribution = $bindable(),
    onBack,
    onNext,
  }: {
    step: 'distribution' | 'service'
    distribution: string
    onBack: () => void
    onNext: () => void
  } = $props()

  let busy = $state(true)
  let error = $state('')
  let distributions = $state<WSLDistribution[] | null>(null)
  let status = $state<WSLDockerStatus | null>(null)
  let busyText = $state<TranslationKey>('settings.docker.wsl_checking.status')

  onMount(() => {
    if (step === 'distribution') void loadDistributions()
    else void checkDistribution()
  })

  /** keeps the picked distro while it is still listed, else picks the default one */
  async function loadDistributions(): Promise<void> {
    try {
      const list = await api.docker.wslDistributions()
      const usable = list.filter((item) => item.version === 2)
      if (!usable.some((item) => item.name === distribution)) {
        distribution = (usable.find((item) => item.default) ?? usable[0])?.name ?? ''
      }
      distributions = list
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      busy = false
    }
  }

  async function run(request: () => Promise<WSLDockerStatus>, text: TranslationKey): Promise<void> {
    busy = true
    busyText = text
    error = ''
    status = null
    try {
      status = await request()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      busy = false
    }
  }

  function checkDistribution(): Promise<void> {
    return run(() => api.docker.wslStatus(distribution), 'settings.docker.wsl_checking.status')
  }

  function setUpDistribution(): Promise<void> {
    return run(() => api.docker.wslSetUp(distribution), 'settings.docker.wsl_setting_up.status')
  }

  const primaryButton = $derived.by((): ModalButton => {
    const next: ModalButton = { label: t('common.actions.next'), buttonStyle: 'prominent', onclick: onNext }
    if (step === 'distribution') return { ...next, disabled: busy || !distribution }
    switch (status?.state) {
      case 'ready':
        return next
      case 'needs_setup':
      case 'systemd_off':
        return { label: t('settings.docker.wsl_setup.action'), buttonStyle: 'prominent', onclick: setUpDistribution, disabled: busy, loading: busy }
      default:
        return { label: t('settings.docker.check_again.action'), buttonStyle: 'prominent', onclick: checkDistribution, disabled: busy, loading: busy }
    }
  })

  function describe(item: WSLDistribution): string {
    if (item.version !== 2) return t('settings.docker.distribution_unsupported.hint')
    const version = tx('settings.docker.distribution_version.label', { version: item.version })
    return item.default ? `${version} · ${t('settings.docker.distribution_default.label')}` : version
  }
</script>

<ModalChrome buttons={[{ label: t('common.actions.back'), buttonStyle: 'neutral', onclick: onBack, disabled: busy }, primaryButton]} />

<VStack gap={4}>
  {#if step === 'distribution'}
    {#if distributions?.length}
      <Text size="sm" foregroundStyle="secondary">{t('settings.docker.distribution.hint')}</Text>
      <ChoiceList options={distributions.map((item) => ({ value: item.name, title: item.name, hint: describe(item), disabled: item.version !== 2 }))} bind:value={distribution} ariaLabel={t('settings.docker.step_distribution.label')} />
    {:else if distributions}
      <ContentUnavailable title={t('settings.docker.distributions.empty')} />
    {/if}
  {:else if busy}
    <ProgressView label={tx(busyText, { distribution })} />
  {:else if status?.state === 'ready'}
    <Banner tint="success" text={tx('settings.docker.wsl_ready.status', { distribution })} />
  {:else if status?.state === 'docker_missing'}
    <Banner tint="danger" text={tx('settings.docker.wsl_missing.status', { distribution })} />
    <Text>{t('settings.docker.wsl_missing.hint')}</Text>
  {:else if status}
    <Text>{tx(status.state === 'systemd_off' ? 'settings.docker.wsl_systemd.hint' : 'settings.docker.wsl_setup.hint', { distribution })}</Text>
    <Banner tint="warning" text={t('settings.docker.wsl_restart.hint')} />
    <Card collapsible open={false} title={t('settings.docker.wsl_commands.title')}>
      <CodeBlock text={status.commands} />
    </Card>
  {/if}

  {#if error}
    <Banner tint="danger" text={error} />
  {/if}
</VStack>
