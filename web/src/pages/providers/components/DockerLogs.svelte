<script lang="ts">
  // Container logs in a modal. Opens following: the output refreshes and
  // stays scrolled to the end until Follow is off.
  import { onMount, tick } from 'svelte'
  import { Banner, CodeBlock, ContentUnavailable, HStack, Toggle, VStack } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { t } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'

  let { pluginId, target }: { pluginId: string; target: string } = $props()

  const FOLLOW_POLL_MS = 2000

  let logs = $state('')
  let loaded = $state(false)
  let error = $state('')
  let following = $state(true)
  let output = $state<HTMLElement>()

  onMount(() => {
    void load()
    const timer = window.setInterval(() => {
      if (following) void load()
    }, FOLLOW_POLL_MS)
    return () => window.clearInterval(timer)
  })

  async function load(): Promise<void> {
    try {
      logs = await api.docker.logs(pluginId, target)
      error = ''
    } catch (e) {
      error = getErrorMessage(e)
      following = false
    }
    loaded = true
    if (following) await scrollToEnd()
  }

  async function scrollToEnd(): Promise<void> {
    await tick()
    const body = output?.querySelector('pre')
    if (body) body.scrollTop = body.scrollHeight
  }

  function setFollowing(next: boolean): void {
    following = next
    if (next) void load()
  }
</script>

<VStack gap={3}>
  <HStack>
    <Toggle checked={following} label={t('providers.docker.follow.label')} onchange={setFollowing} />
  </HStack>
  {#if error}
    <Banner tint="danger" text={error} />
  {/if}
  {#if loaded && !logs.trim()}
    <ContentUnavailable title={t('providers.docker.logs.empty')} />
  {:else}
    <div bind:this={output}>
      <CodeBlock text={logs} lang="log" maxHeight="60vh" />
    </div>
  {/if}
</VStack>
