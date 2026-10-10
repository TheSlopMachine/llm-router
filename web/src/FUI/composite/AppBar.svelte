<script lang="ts">
  import type { Snippet } from 'svelte'
  import Text from '../controls/Text.svelte'
  import Icon from '../controls/Icon.svelte'
  import Button from '../controls/Button.svelte'
  import FloatingView from '../controls/FloatingView.svelte'
  import Toolbar from './Toolbar.svelte'

  export interface Crumb {
    label: string
    href: string
  }

  // HStack { leading, VStack { crumbs / title(+info) / subtitle }, actions }, centered vertically.
  // Fixed row heights make the bar one of four constant heights: title / +crumbs / +subtitle / both.
  // Actions degrade in steps as the bar narrows: every button, then primary + "more" menu,
  // then everything in the menu, and only then do the titles start to truncate.
  let {
    title,
    crumbs = [],
    subtitle,
    info,
    infoLabel = 'Info',
    leading,
    actions,
  } = $props<{
    title: string
    crumbs?: Crumb[]
    /** visible secondary line (counts, type key, ...) */
    subtitle?: string | Snippet
    /** description in a popover behind an info button (only rendered when given) */
    info?: string | Snippet
    infoLabel?: string
    leading?: Snippet
    actions?: Snippet
  }>()

  let infoOpen = $state(false)
  let infoAnchor = $state<HTMLElement>()
</script>

{#snippet text(v: string | Snippet, variant: 'subtitle')}
  {#if typeof v === 'function'}
    <Text {variant}>{@render v()}</Text>
  {:else}
    <Text {variant} text={v} />
  {/if}
{/snippet}

<header class="appbar">
  <div class="ab-inner">
    {#if leading}<div class="ab-leading">{@render leading()}</div>{/if}
    <div class="ab-titles">
      {#if crumbs.length > 0}
        <nav class="ab-row ab-crumbs" aria-label="Breadcrumb">
          <span class="ab-back"><Icon name="arrow_left" size="md" /></span>
          {#each crumbs as c, i (c.href)}
            {#if i > 0}<span class="ab-sep" aria-hidden="true">/</span>{/if}
            <a class="ab-crumb" href={c.href}>{c.label}</a>
          {/each}
        </nav>
      {/if}
      <div class="ab-row ab-title-row">
        <span class="ab-title"><Text variant="page-title" text={title} /></span>
        {#if info}
          <span class="ab-info" bind:this={infoAnchor}>
            <Button
              style="text"
              size="small"
              icon={{ name: 'info' }}
              ariaLabel={infoLabel}
              title={infoLabel}
              ariaExpanded={infoOpen}
              onclick={() => (infoOpen = !infoOpen)}
            />
          </span>
        {/if}
      </div>
      {#if subtitle}
        <div class="ab-row ab-sub">{@render text(subtitle, 'subtitle')}</div>
      {/if}
    </div>
    {#if actions}
      <div class="ab-actions"><Toolbar overflow="menu" halign="end" collapseButtons={false}>{@render actions()}</Toolbar></div>
    {/if}
  </div>
  {#if info}
    <FloatingView open={infoOpen} anchor={infoAnchor} onclose={() => (infoOpen = false)} label={infoLabel}>
      {#snippet children()}
        <div class="ab-info-body">{@render text(info, 'subtitle')}</div>
      {/snippet}
    </FloatingView>
  {/if}
</header>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .appbar {
    flex-shrink: 0;
    padding: var(--fui-page-pad, var(--fui-space-5));
    padding-top: calc(var(--fui-page-pad, var(--fui-space-5)) + env(safe-area-inset-top));
    padding-left: calc(var(--fui-page-pad, var(--fui-space-5)) + env(safe-area-inset-left));
    padding-right: calc(var(--fui-page-pad, var(--fui-space-5)) + env(safe-area-inset-right));
    background: var(--fui-elev);
    min-width: 0;
  }
  .ab-inner {
    display: flex;
    align-items: center;
    gap: var(--fui-space-4);
    width: 100%;
    max-width: var(--fui-content-max-w);
    min-width: 0;
    min-height: var(--fui-ctl-large);
    margin: 0 auto;
  }
  .ab-leading { flex-shrink: 0; display: flex; align-items: center; }
  /* titles truncate last: they only shrink once the actions are down to the "more" button */
  .ab-titles { display: flex; flex-direction: column; min-width: 0; flex: 0 1 auto; }
  .ab-actions { flex: 1 1 0; min-width: var(--fui-ctl-medium); }
  .ab-row { display: flex; align-items: center; min-width: 0; }
  /* one text line each; the title row is as tall as the info button, so a bar is one of a few fixed heights */
  .ab-crumbs, .ab-sub { height: calc(var(--fui-text-sm) * var(--fui-leading-base)); gap: var(--fui-space-1); }
  .ab-crumbs { flex-wrap: nowrap; }
  .ab-title-row { min-height: var(--fui-ctl-small); gap: var(--fui-space-2); }
  .ab-title, .ab-sub { min-width: 0; }
  .ab-title :global(*), .ab-sub :global(*), .ab-crumb {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .ab-title { flex: 0 1 auto; }
  .ab-crumbs { color: var(--fui-color-text-soft); position: relative; }
  /* hangs in the gutter so crumb, title and subtitle all start at the same x */
  .ab-back { position: absolute; right: 100%; display: inline-flex; }
  .ab-sep { font-size: var(--fui-text-sm); }
  .ab-crumb {
    font-size: var(--fui-text-sm);
    color: inherit;
    text-decoration: none;
    flex: 0 1 auto;
  }
  .ab-crumb:hover { color: var(--fui-color-text); text-decoration: underline; }
  .ab-info { display: inline-flex; flex-shrink: 0; }
  .ab-info-body { max-width: var(--fui-popover-w-md); }
</style>
