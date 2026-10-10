<script lang="ts">
  import type { Snippet } from 'svelte'
  import Text from '../controls/Text.svelte'
  import { getAppBarState } from '../core/appbar.svelte'

  // level="page": when an app bar exists, the title/subtitle/actions are handed to it
  // (the page keeps writing <Header> exactly as before) and nothing renders here.
  // Without an app bar (or level="section") it renders in place.
  let {
    title,
    subtitle,
    info,
    level = 'page',
    leading,
    actions
  } = $props<{
    title: string
    /** visible secondary line under the title */
    subtitle?: string | Snippet
    /** description behind the (i) button; rendered under the title when there is no app bar */
    info?: string | Snippet
    level?: 'page' | 'section'
    leading?: Snippet
    actions?: Snippet
  }>()

  const bar = level === 'page' ? getAppBarState() : null
  const id = Symbol('header')
  $effect(() => {
    if (!bar) return
    bar.claim(id, { title, subtitle, info, actions })
    return () => bar.release(id)
  })

  const variant = $derived(level === 'section' ? 'section-title' : 'page-title')
  const subSnippet = $derived(typeof subtitle === 'function')
  const infoSnippet = $derived(typeof info === 'function')
</script>

{#if !bar}
  <div class="hdr">
    {#if leading}
      <div class="hdr-leading">{@render leading()}</div>
    {/if}
    <div class="hdr-title">
      <Text {variant} text={title} />
      {#if subtitle}
        {#if subSnippet}
          <div class="hdr-subtitle"><Text variant="subtitle">{@render (subtitle as Snippet)()}</Text></div>
        {:else}
          <Text variant="subtitle" text={subtitle as string} />
        {/if}
      {/if}
      {#if info}
        {#if infoSnippet}
          <div class="hdr-subtitle"><Text variant="subtitle">{@render (info as Snippet)()}</Text></div>
        {:else}
          <Text variant="subtitle" text={info as string} />
        {/if}
      {/if}
    </div>
    {#if actions}
      <div class="hdr-actions">{@render actions()}</div>
    {/if}
  </div>
{/if}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .hdr {
    display: flex;
    align-items: center;
    gap: var(--fui-space-4);
    flex-wrap: wrap;
    min-width: 0;
  }
  .hdr-leading { flex-shrink: 0; display: flex; align-items: center; }
  .hdr-title {
    flex: 1 1 var(--fui-header-title-basis);
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-1);
  }
  .hdr-subtitle { min-width: 0; }
  .hdr-actions { flex-shrink: 0; display: flex; align-items: center; gap: var(--fui-space-3); flex-wrap: wrap; }
</style>
