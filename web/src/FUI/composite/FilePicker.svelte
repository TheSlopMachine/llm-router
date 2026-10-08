<script lang="ts">
  import Button from '../controls/Button.svelte'

  // Shared file picker. Unifies the hidden input + Import button pattern in
  // SettingsPage (subsystem + provider import) and InstalledTab (.lua upload).
  let {
    accept = '.json',
    label = 'Import file',
    buttonText = 'Import',
    icon,
    disabled = false,
    onPick,
  } = $props<{
    accept?: string
    label?: string
    buttonText?: string
    icon?: string
    disabled?: boolean
    onPick: (file: File) => void
  }>()

  let input = $state<HTMLInputElement>()
  let filename = $state('')

  function handleChange(): void {
    const file = input?.files?.[0]
    if (!file) return
    filename = file.name
    onPick(file)
    if (input) input.value = ''
  }
</script>

<input
  bind:this={input}
  type="file"
  {accept}
  aria-label={label}
  class="file-input"
  onchange={handleChange} />
<Button
  text={filename && !disabled ? `${buttonText}: ${filename}` : buttonText}
  icon={icon ? { name: icon } : undefined}
  disabled={disabled}
  onclick={() => input?.click()} />

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(input) { font: inherit; color: inherit; }
  .file-input {
    display: none;
  }
</style>
