<script lang="ts">
  import Button from '../controls/Button.svelte'
  import type { Size } from '../tokens'
  import { fuiText } from '../core/texts'

  // CopyButton copies a short identifier (model id, key) with a check-mark
  // feedback. Single implementation for every id-copy button; code and
  // prose copying live with their own widgets.
  let {
    text,
    size = 'small',
    title = fuiText('copy'),
    ariaLabel = fuiText('copy'),
    disabled = false,
  } = $props<{
    text: string
    size?: Size
    title?: string
    ariaLabel?: string
    disabled?: boolean
  }>()

  let copied = $state(false)
  let copyTimer: ReturnType<typeof setTimeout> | undefined = undefined

  function copy(): void {
    void navigator.clipboard.writeText(text).then(() => {
      copied = true
      if (copyTimer !== undefined) clearTimeout(copyTimer)
      copyTimer = setTimeout(() => {
        copied = false
      }, 1500)
    }).catch(() => {})
  }
</script>

<Button
  style="text"
  {size}
  icon={{ name: copied ? 'check' : 'content_copy' }}
  {title}
  {ariaLabel}
  {disabled}
  onclick={() => copy()}
/>
