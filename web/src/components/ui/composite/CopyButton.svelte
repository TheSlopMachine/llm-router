<script lang="ts">
  import Button from '../controls/Button.svelte'
  import type { Size } from '../tokens'
  import { t } from '../../../lib/i18n.svelte'

  // CopyButton copies a short identifier (model id, key) with a check-mark
  // feedback. Single implementation for every id-copy button; code and
  // prose copying live with their own widgets.
  let {
    text,
    size = 'small',
    title = t('Copy model id'),
    ariaLabel = t('Copy model id'),
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
