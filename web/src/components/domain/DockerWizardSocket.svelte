<!-- The socket step of the Docker wizard: finds Docker on the router machine. Only Linux and macOS routers offer it. -->
<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { Banner, Button, CodeBlock, ContentUnavailable, Field, HStack, TextField, VStack, type ModalButton } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { t, tx } from '$lib/i18n.svelte'
  import type { TranslationKey } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import type { LocalDocker, LocalDockerSocket } from '$lib/types'
  import DockerChoice from './DockerChoice.svelte'

  let {
    socket = $bindable(),
    updateButtons,
    onBack,
    onNext,
  }: {
    socket: string
    updateButtons: (buttons: ModalButton[]) => void
    onBack: () => void
    onNext: () => void
  } = $props()

  let busy = $state(true)
  let error = $state('')
  let local = $state<LocalDocker | null>(null)
  let otherPath = $state('')
  let selected = $derived(local?.sockets.find((item) => item.path === socket))

  const sourceKeys: Record<Exclude<LocalDockerSocket['source'], 'context'>, TranslationKey> = {
    docker_host: 'settings.docker.socket_source_docker_host.label',
    system: 'settings.docker.socket_source_system.label',
    rootless: 'settings.docker.socket_source_rootless.label',
    podman: 'settings.docker.socket_source_podman.label',
    podman_rootless: 'settings.docker.socket_source_podman_rootless.label',
    custom: 'settings.docker.socket_source_custom.label',
  }
  const stateKeys: Record<LocalDockerSocket['state'], TranslationKey> = {
    ready: 'settings.docker.socket_ready.status',
    permission_denied: 'settings.docker.socket_permission_denied.status',
    not_running: 'settings.docker.socket_not_running.status',
    missing: 'settings.docker.socket_missing.status',
    unreachable: 'settings.docker.socket_unreachable.status',
  }

  onMount(() => void load())

  /** finds the sockets again; keeps the picked one listed, else picks the first that answers */
  async function load(): Promise<void> {
    busy = true
    error = ''
    try {
      const found = await api.docker.localDocker()
      if (socket && !found.sockets.some((item) => item.path === socket)) {
        found.sockets.push(await api.docker.probeLocalSocket(socket))
      }
      if (!found.sockets.some((item) => item.path === socket)) {
        socket = (found.sockets.find((item) => item.state === 'ready') ?? found.sockets[0])?.path ?? ''
      }
      local = found
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      busy = false
    }
  }

  async function checkOtherPath(found: LocalDocker): Promise<void> {
    busy = true
    error = ''
    try {
      const probed = await api.docker.probeLocalSocket(otherPath.trim())
      local = { ...found, sockets: [...found.sockets.filter((item) => item.path !== probed.path), probed] }
      socket = probed.path
      otherPath = ''
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      busy = false
    }
  }

  function primaryButton(): ModalButton {
    if (selected?.state === 'ready') return { label: t('common.actions.next'), buttonStyle: 'prominent', onclick: onNext, disabled: busy }
    return { label: t('settings.docker.check_again.action'), buttonStyle: 'prominent', onclick: () => void load(), disabled: busy, loading: busy }
  }

  $effect(() => {
    void busy; void selected
    untrack(() => updateButtons([{ label: t('common.actions.back'), buttonStyle: 'neutral', onclick: onBack, disabled: busy }, primaryButton()]))
  })

  function describe(item: LocalDockerSocket): string {
    const source = item.source === 'context' ? tx('settings.docker.socket_source_context.label', { context: item.context }) : t(sourceKeys[item.source])
    return `${source} · ${t(stateKeys[item.state])}`
  }
</script>

<VStack gap={4}>
  {#if !local && busy}
    <ContentUnavailable title={t('settings.docker.sockets_checking.status')} />
  {:else if local}
    {@const found = local}
    {#if local.sockets.length}
      <VStack gap={2}>
        {#each local.sockets as item (item.path)}
          <DockerChoice title={item.path} hint={describe(item)} selected={socket === item.path} onclick={() => { socket = item.path }} />
        {/each}
      </VStack>
    {:else}
      <ContentUnavailable title={t('settings.docker.sockets.empty')} />
      {#if local.commands}
        <CodeBlock text={local.commands} />
      {/if}
    {/if}

    {#if selected?.state === 'permission_denied'}
      <Banner tint="warning" text={tx('settings.docker.socket_permission_denied.hint', { user: local.user })} />
    {:else if selected?.state === 'not_running'}
      <Banner tint="warning" text={t('settings.docker.socket_not_running.hint')} />
    {:else if selected?.state === 'missing'}
      <Banner tint="danger" text={t('settings.docker.socket_missing.hint')} />
    {:else if selected?.state === 'unreachable'}
      <Banner tint="danger" text={selected.error} />
    {/if}
    {#if selected?.commands}
      <CodeBlock text={selected.commands} />
    {/if}

    <Field label={t('settings.docker.socket_other.label')}>
      <HStack gap={2}>
        <TextField bind:value={otherPath} placeholder="/var/run/docker.sock" size="lg" autocomplete="off" />
        <Button text={t('settings.docker.socket_check.action')} buttonStyle="neutral" size="lg" disabled={busy || !otherPath.trim().startsWith('/')} onclick={() => void checkOtherPath(found)} />
      </HStack>
    </Field>
  {/if}

  {#if error}
    <Banner tint="danger" text={error} />
  {/if}
</VStack>
