<script lang="ts">
  import Dropdown from './Dropdown.svelte'

  let {
    actions = [],
    label = 'Actions',
    disabled = false,
    rounded = 'sm',
    onaction
  } = $props<{
    actions: Array<{ id: string; label: string; icon?: string; disabled?: boolean }>
    label?: string
    disabled?: boolean
    rounded?: 'sm' | 'lg'
    onaction?: (id: string) => void
  }>()
</script>

<Dropdown {actions} {label} {disabled} {rounded} {onaction} />
