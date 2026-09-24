<script lang="ts">
  import { squircle } from '../../../lib/squircle'
  import Button from '../controls/Button.svelte'

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

<div class="code-block">
  {#if label}<div class="code-label">{label}</div>{/if}
  <div class="code-row" use:squircle={12}>
    <code class="code-text">{text}</code>
    <Button
      size="small"
      icon={{ name: copied ? 'check' : 'content_copy' }}
      title="Copy"
      ariaLabel="Copy code"
      onclick={() => void copyText()}
    />
  </div>
</div>

<style>
  .code-block {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .code-label {
    font-size: var(--text-sm);
    font-weight: 500;
  }
  .code-row {
    display: flex;
    gap: var(--space-3);
    align-items: center;
    padding: var(--space-3) var(--space-4);
    border: none;
    border-radius: var(--ctl-radius);
    background: var(--elev);
  }
  .code-text {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: var(--text-md);
    letter-spacing: 0.04em;
    overflow-wrap: anywhere;
  }
</style>
