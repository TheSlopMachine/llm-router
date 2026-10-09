<script lang="ts">
  import Button from '../controls/Button.svelte'
  import HStack from '../layout/HStack.svelte'
  import VStack from '../layout/VStack.svelte'
  import Text from '../controls/Text.svelte'

  // Shared destructive confirm card for FloatingView popovers. Replaces the
  // ~10 hand-rolled Cancel + red Delete blocks across Tokens, Providers,
  // Proxy, VirtualModels, Plugins.
  let {
    title,
    body = 'This action cannot be undone.',
    confirmLabel = 'Delete',
    busy = false,
    busyLabel,
    onCancel,
    onConfirm,
  } = $props<{
    title: string
    body?: string
    confirmLabel?: string
    busy?: boolean
    busyLabel?: string
    onCancel: () => void
    onConfirm: () => void
  }>()

  let pendingLabel = $derived(busyLabel ?? `${confirmLabel}…`)
</script>

<VStack gap={3}>
  <VStack gap={1}>
    <Text weight="medium">{title}</Text>
    <Text size="sm" tone="soft">{body}</Text>
  </VStack>
  <HStack justify="end" gap={2}>
    <Button style="text" text="Cancel" onclick={onCancel} disabled={busy} />
    <Button
      style="prominent"
      tint="var(--fui-color-danger)"
      text={busy ? pendingLabel : confirmLabel}
      onclick={onConfirm}
      disabled={busy} />
  </HStack>
</VStack>
