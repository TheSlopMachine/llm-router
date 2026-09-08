<script lang="ts">
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
  <div class="code-row">
    <code class="code-text">{text}</code>
    <button
      type="button"
      class="btn-icon"
      onclick={() => void copyText()}
      aria-label="Copy code"
      title="Copy"
    >
      <span class="icon">{copied ? 'check' : 'content_copy'}</span>
    </button>
  </div>
</div>

<style>
  .code-block {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .code-label {
    font-size: 13px;
    font-weight: 500;
  }
  .code-row {
    display: flex;
    gap: 8px;
    align-items: center;
    padding: 10px 12px;
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
    background: var(--color-surface-container, #f4f5f5);
  }
  .code-text {
    flex: 1;
    min-width: 0;
    font-family: "DM Mono", "SF Mono", monospace;
    font-size: 16px;
    letter-spacing: 0.04em;
    overflow-wrap: anywhere;
  }
</style>
