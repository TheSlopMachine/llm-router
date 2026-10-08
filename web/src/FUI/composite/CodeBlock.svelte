<script lang="ts">
  import Button from '../controls/Button.svelte'
  import Text from '../controls/Text.svelte'
  import Box from '../layout/Box.svelte'
  import HStack from '../layout/HStack.svelte'
  import VStack from '../layout/VStack.svelte'

  let { text, label = '' } = $props<{
    text: string
    label?: string
  }>()

  let copied = $state(false)
  let copyTimer: ReturnType<typeof setTimeout> | undefined = undefined

  async function copyText(): Promise<void> {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      const area = document.createElement('textarea')
      area.value = text
      document.body.appendChild(area)
      area.select()
      document.execCommand('copy')
      document.body.removeChild(area)
    }
    copied = true
    if (copyTimer !== undefined) clearTimeout(copyTimer)
    copyTimer = setTimeout(() => {
      copied = false
    }, 1500)
  }
</script>

<VStack gap={2}>
  {#if label}<Text size="sm" weight="medium">{label}</Text>{/if}
  <!-- radius-md + uniform pad: matches Toasts geometry; was ctl-radius + 12/16px pad -->
  <Box elev pad={3} radius="md" squircled>
    <HStack gap={3} align="center">
      <code class="code-text">{text}</code>
      <Button
        size="small"
        icon={{ name: copied ? 'check' : 'content_copy' }}
        title="Copy"
        ariaLabel="Copy code"
        onclick={() => void copyText()}
      />
    </HStack>
  </Box>
</VStack>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .code-text {
    flex: 1;
    min-width: 0;
    font-family: var(--fui-font-mono);
    font-size: var(--fui-text-md);
    letter-spacing: 0.04em;
    overflow-wrap: anywhere;
  }
</style>
