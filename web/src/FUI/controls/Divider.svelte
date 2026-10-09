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
    style:margin="0 {space(margin) ?? 'var(--fui-space-0)'}"
  ></div>
{:else}
  <hr class="divider {cls}" style:margin="{space(margin) ?? 'var(--fui-space-0)'} 0" />
{/if}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .divider {
    border: none;
    border-top: var(--fui-border-w) solid var(--fui-color-outline-soft);
    margin: var(--fui-space-5) 0;
  }
  .divider-v {
    width: var(--fui-border-w);
    align-self: stretch;
    background: var(--fui-color-outline-soft);
    flex: none;
  }
</style>
