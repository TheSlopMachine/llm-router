<script lang="ts">
  import Icon from './Icon.svelte'
  import { squircleAuto } from '../core/squircle-baked'

  // Static tag. Never interactive: toggling is a small Button's job.
  // `color` keeps the historical 'chip-<hue>' strings (lib/capabilities,
  // lib/modalities and pages pass them); the hue becomes a data attribute so
  // no class name has to exist outside this file.
  // Corners: use:squircle on the root (native corner-shape / clip-path / radius).
  let {
    icon = '',
    text = '',
    iconSide = 'left',
    color = 'chip-neutral',
    size = 'medium',
    title = '',
  } = $props<{
    icon?: string
    text?: string
    iconSide?: 'left' | 'right'
    color?: string
    size?: 'small' | 'medium' | 'large'
    title?: string
  }>()

  const hue = $derived(color.replace(/^chip-/, ''))
</script>

<span use:squircleAuto={{ bake: text ? undefined : `chip-icon-${size}`, fonts: true }} class="chip" data-hue={hue} data-size={size} class:icon-only={!text} {title}>
  {#if icon && iconSide === 'left'}<Icon name={icon} />{/if}
  {#if text}<span class="label">{text}</span>{/if}
  {#if icon && iconSide === 'right'}<Icon name={icon} />{/if}
</span>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .chip {
    --_bg: var(--fui-color-chip-neutral-bg);
    --_fg: var(--fui-color-chip-neutral-text);

    display: inline-flex;
    align-items: center;
    gap: var(--fui-space-2);
    padding: var(--fui-chip-pad-y) var(--fui-chip-pad-x);
    border-radius: var(--fui-radius-md);
    font-size: var(--fui-text-sm);
    font-weight: 500;
    line-height: var(--fui-chip-line);
    white-space: nowrap;
    background: var(--_bg);
    color: var(--_fg);
  }
  .chip :global(.icon) { font-size: var(--fui-text-sm); }
  .label { line-height: 1; }

  /* hues: one place, tokens only */
  .chip[data-hue='blue']   { --_bg: var(--fui-color-badge-blue-bg);   --_fg: var(--fui-color-badge-blue-text); }
  .chip[data-hue='green']  { --_bg: var(--fui-color-badge-green-bg);  --_fg: var(--fui-color-badge-green-text); }
  .chip[data-hue='yellow'] { --_bg: var(--fui-color-badge-yellow-bg); --_fg: var(--fui-color-badge-yellow-text); }
  .chip[data-hue='red']    { --_bg: var(--fui-color-badge-red-bg);    --_fg: var(--fui-color-badge-red-text); }
  .chip[data-hue='purple'] { --_bg: var(--fui-color-badge-purple-bg); --_fg: var(--fui-color-badge-purple-text); }
  .chip[data-hue='teal']   { --_bg: var(--fui-color-badge-teal-bg);   --_fg: var(--fui-color-badge-teal-text); }
  .chip[data-hue='orange'] { --_bg: var(--fui-color-badge-orange-bg); --_fg: var(--fui-color-badge-orange-text); }

  /* sizes: sm 20px / base 24px / lg 28px row height */
  .chip[data-size='small'] {
    padding: var(--fui-chip-sm-pad-y) var(--fui-chip-sm-pad-x);
    border-radius: var(--fui-chip-sm-radius);
    font-size: var(--fui-chip-sm-text);
    line-height: var(--fui-chip-sm-line);
  }
  .chip[data-size='small'] :global(.icon) { font-size: var(--fui-chip-sm-text); }
  .chip[data-size='large'] {
    padding: var(--fui-chip-md-pad-y) var(--fui-chip-md-pad-x);
    border-radius: var(--fui-chip-md-radius);
    font-size: var(--fui-text-base);
    line-height: var(--fui-chip-md-line);
  }
  .chip[data-size='large'] :global(.icon) { font-size: var(--fui-text-base); }

  /* icon-only: geometry scales with size */
  .chip.icon-only { padding: var(--fui-chip-icon-pad); border-radius: var(--fui-chip-icon-radius); }
  .chip.icon-only[data-size='small'] { padding: var(--fui-chip-icon-sm-pad); border-radius: var(--fui-chip-icon-sm-radius); }
  .chip.icon-only[data-size='large'] { padding: var(--fui-chip-icon-lg-pad); border-radius: var(--fui-chip-icon-lg-radius); }
</style>
