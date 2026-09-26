<script lang="ts">
  import { space, type Step } from '../tokens'

  let {
    axis = 'h',
    margin,
    class: cls = '',
  } = $props<{ axis?: 'h' | 'v'; margin?: Step; class?: string }>()
</script>

{#if axis === 'v'}
  <div
    class="divider-v {cls}"
    role="separator"
    aria-orientation="vertical"
    style:margin="0 {space(margin) ?? '0px'}"
  ></div>
{:else}
  <hr class="divider {cls}" style:margin="{space(margin) ?? '0px'} 0" />
{/if}

<style>
  .divider {
    border: none;
    border-top: 1px solid var(--color-outline-soft);
    margin: var(--space-5) 0;
  }
  .divider-v {
    width: 1px;
    align-self: stretch;
    background: var(--color-outline-soft);
    flex: none;
  }
</style>
