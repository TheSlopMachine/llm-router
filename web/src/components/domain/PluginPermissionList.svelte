<script lang="ts">
  // The permissions of a plugin: network hosts and, when it needs Docker, its
  // container limits. A diff marks what an update changes.
  import { Chip, HStack, Text, VStack } from '@fuckyouryan/fui'
  import { t, tx } from '$lib/i18n.svelte'
  import type { PermissionDiff } from '$lib/plugin-permissions'
  import type { DockerDirectives } from '$lib/types'

  let { hosts, unsafe, docker, diff = null }: {
    hosts: string[]
    unsafe: boolean
    docker: DockerDirectives | null
    diff?: PermissionDiff | null
  } = $props()
</script>

<VStack>
  <VStack spacing="tight">
    <Text tag="h3" size="sm" weight="bold">{t('plugins.permissions.hosts.title')}</Text>
    {#if unsafe}
      <div><Chip text={t('plugins.network.unrestricted')} tint="danger" size="sm" /></div>
    {:else if hosts.length === 0}
      <Text size="sm" foregroundStyle="secondary">{t('plugins.network.none')}</Text>
    {:else}
      {#each hosts as host (host)}
        {#if diff?.added.includes(host)}
          <Text size="xs" mono>+ {host}</Text>
        {:else}
          <Text size="xs" mono foregroundStyle="secondary">{host}</Text>
        {/if}
      {/each}
    {/if}
    {#each diff?.removed ?? [] as host (host)}
      <Text size="xs" mono foregroundStyle="tertiary">− {host}</Text>
    {/each}
  </VStack>
  {#if docker}
    <VStack spacing="tight">
      <HStack spacing="tight" valign="center">
        <Text tag="h3" size="sm" weight="bold">{t('plugins.docker.label')}</Text>
        {#if diff?.dockerChanged}
          <Chip text={t('plugins.permissions.changed.status')} tint="warning" size="sm" />
        {/if}
      </HStack>
      <Text size="sm" foregroundStyle="secondary">{t('plugins.permissions.docker.desc')}</Text>
      <Text size="sm">{tx('plugins.permissions.memory.label', { value: docker.memory_limit_mb })}</Text>
      <Text size="sm">{tx('plugins.permissions.cpu.label', { value: docker.cpu_limit })}</Text>
      <Text size="sm">{tx('plugins.permissions.container_limit.label', { value: docker.container_limit })}</Text>
      {#if docker.gpu_limit > 0}
        <Text size="sm">{tx('plugins.permissions.gpu.label', { value: docker.gpu_limit })}</Text>
      {/if}
    </VStack>
  {/if}
</VStack>
