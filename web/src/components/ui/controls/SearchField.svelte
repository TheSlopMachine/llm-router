<script lang="ts">
  // Search box: a thin specialisation of TextEdit -- a fixed search glyph as
  // the leading icon. Clear and focus animations live in TextEdit.
  import TextEdit from './TextEdit.svelte'

  let {
    value = $bindable(''),
    placeholder = 'Search...',
    disabled = false,
  } = $props<{
    value?: string
    placeholder?: string
    disabled?: boolean
  }>()
</script>

<TextEdit
  bind:value
  hint={placeholder}
  {disabled}
  leadingIcon="search"
/>
