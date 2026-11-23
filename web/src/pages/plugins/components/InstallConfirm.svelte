<script lang="ts">
  import { Button, HStack, Text, VStack } from '@fuckyouryan/fui'
  import PluginPermissionList from '../../../components/domain/PluginPermissionList.svelte'
  import { t } from '$lib/i18n.svelte'
  import type { PermissionDiff } from '$lib/plugin-permissions'
  import type { PluginPermissions } from '$lib/types'

  let {
    permissions,
    diff = null,
    confirmLabel,
    busy = false,
    oncancel,
    onconfirm
  }: {
    permissions: PluginPermissions
    diff?: PermissionDiff | null
    confirmLabel: string
    busy?: boolean
    oncancel: () => void
    onconfirm: () => void
  } = $props()
</script>

<VStack gap={3}>
  <Text weight="medium" size="md">{t('plugins.permissions_requested')}</Text>
  <PluginPermissionList hosts={permissions.allow_hosts} unsafe={permissions.unsafe} docker={permissions.docker} {diff} />
  <HStack halign="end" gap={3}>
    <Button size="sm" buttonStyle="text" onclick={oncancel} disabled={busy}>{t('common.actions.cancel')}</Button>
    <Button size="sm" buttonStyle="prominent" onclick={onconfirm} disabled={busy}>
      {confirmLabel}
    </Button>
  </HStack>
</VStack>
