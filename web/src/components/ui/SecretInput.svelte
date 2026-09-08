<script lang="ts">
  let {
    id,
    label = '',
    required = false,
    placeholder = '',
    value = '',
    disabled = false,
    oninput,
  } = $props<{
    id: string
    label?: string
    required?: boolean
    placeholder?: string
    value?: string
    disabled?: boolean
    oninput?: (value: string) => void
  }>()

  let revealed = $state(false)
  let copied = $state(false)
  let copyTimer: ReturnType<typeof setTimeout> | undefined = undefined

  function toggleReveal(): void {
    revealed = !revealed
  }

  async function copyValue(): Promise<void> {
    if (!value) return
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      const area = document.createElement('textarea')
      area.value = value
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

<div class="form-group">
  {#if label}<label for={id}>{label}{#if required} *{/if}</label>{/if}
  <div class="secret-row">
    <input
      {id}
      type={revealed ? 'text' : 'password'}
      value={value ?? ''}
      oninput={(e) => oninput?.((e.target as HTMLInputElement).value)}
      {placeholder}
      {required}
      {disabled}
      autocomplete="new-password"
      spellcheck={false}
    />
    <button
      type="button"
      class="btn-icon"
      onclick={toggleReveal}
      aria-label={revealed ? 'Hide secret' : 'Show secret'}
      aria-pressed={revealed}
      title={revealed ? 'Hide' : 'Show'}
    >
      <span class="icon">{revealed ? 'visibility_off' : 'visibility'}</span>
    </button>
    <button
      type="button"
      class="btn-icon"
      onclick={() => void copyValue()}
      disabled={!value || disabled}
      aria-label="Copy secret"
      title="Copy"
    >
      <span class="icon">{copied ? 'check' : 'content_copy'}</span>
    </button>
  </div>
</div>

<style>
  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .form-group label {
    font-size: 13px;
    font-weight: 500;
  }
  .secret-row {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .secret-row input {
    flex: 1;
    min-width: 0;
  }
</style>
