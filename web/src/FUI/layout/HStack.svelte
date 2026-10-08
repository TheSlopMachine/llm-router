<script lang="ts">
  import Stack from './Stack.svelte'
  let { children, ...rest } = $props<{ children: import('svelte').Snippet; [k: string]: unknown }>()
</script>

<Stack axis="h" {...rest}>{@render children()}</Stack>
