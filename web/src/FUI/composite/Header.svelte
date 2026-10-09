<script lang="ts">
  import type { Snippet } from 'svelte'
  import Text from '../controls/Text.svelte'

  let {
    title,
    subtitle,
    level = 'page',
    leading,
    actions
  } = $props<{
    title: string
    subtitle?: string | Snippet
    level?: 'page' | 'section'
    leading?: Snippet
    actions?: Snippet
  }>()

  const variant = $derived(level === 'section' ? 'section-title' : 'page-title')
  const isSnippet = $derived(typeof subtitle === 'function')
</script>

<div class="hdr">
  {#if leading}
    <div class="hdr-leading">{@render leading()}</div>
  {/if}
  <div class="hdr-title">
    <Text {variant} text={title} />
    {#if subtitle}
      {#if isSnippet}
        <div class="hdr-subtitle"><Text variant="subtitle">{@render (subtitle as Snippet)()}</Text></div>
      {:else}
        <Text variant="subtitle" text={subtitle as string} />
      {/if}
    {/if}
  </div>
  {#if actions}
    <div class="hdr-actions">{@render actions()}</div>
  {/if}
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .hdr {
    display: flex;
    align-items: center;
    gap: var(--fui-space-4);
    flex-wrap: wrap;
    min-width: 0;
  }
  .hdr-leading {
    flex-shrink: 0;
    display: flex;
    align-items: center;
  }
  .hdr-title {
    flex: 1 1 var(--fui-header-title-basis);
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-1);
  }
  .hdr-subtitle {
    min-width: 0;
  }
  .hdr-actions {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: var(--fui-space-3);
    flex-wrap: wrap;
  }
</style>
