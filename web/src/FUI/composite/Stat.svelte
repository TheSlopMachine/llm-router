<script lang="ts">
  import type { Snippet } from 'svelte'
  import Text from '../controls/Text.svelte'

  let { label, value, tone, children } = $props<{
    label: string
    value?: string | number
    tone?: 'default' | 'soft' | 'disabled' | 'accent' | 'danger' | 'success' | 'warning'
    children?: Snippet
  }>()
</script>

<div class="stat">
  <Text variant="caption" text={label} />
  {#if children}
    {@render children()}
  {:else}
    <Text variant="value" {tone} text={String(value ?? '')} />
  {/if}
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .stat {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--fui-space-1);
    min-width: 0;
    overflow-wrap: anywhere;
  }
</style>
