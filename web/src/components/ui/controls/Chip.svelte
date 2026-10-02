<script lang="ts">
  import Icon from './Icon.svelte'

  // Static tag. Never interactive: toggling is a small Button's job.
  // `color` keeps the historical 'chip-<hue>' strings (lib/capabilities,
  // lib/modalities and pages pass them); the hue becomes a data attribute so
  // no class name has to exist outside this file.
  // Corners come from startAutoSquircle (main.ts), which hooks the `.chip`
  // class; keep that class on the root.
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

<span class="chip" data-hue={hue} data-size={size} class:icon-only={!text} {title}>
  {#if icon && iconSide === 'left'}<Icon name={icon} />{/if}
  {#if text}<span class="label">{text}</span>{/if}
  {#if icon && iconSide === 'right'}<Icon name={icon} />{/if}
</span>

<style>
  .chip {
    --_bg: var(--color-chip-neutral-bg);
    --_fg: var(--color-chip-neutral-text);

    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: 6px 12px;
    border-radius: var(--radius-md);
    font-size: var(--text-sm);
    font-weight: 500;
    line-height: 18px;
    white-space: nowrap;
    background: var(--_bg);
    color: var(--_fg);
  }
  .chip :global(.icon) { font-size: var(--text-sm); }
  .label { line-height: 1; }

  /* hues: one place, tokens only */
  .chip[data-hue='blue']   { --_bg: var(--color-badge-blue-bg);   --_fg: var(--color-badge-blue-text); }
  .chip[data-hue='green']  { --_bg: var(--color-badge-green-bg);  --_fg: var(--color-badge-green-text); }
  .chip[data-hue='yellow'] { --_bg: var(--color-badge-yellow-bg); --_fg: var(--color-badge-yellow-text); }
  .chip[data-hue='red']    { --_bg: var(--color-badge-red-bg);    --_fg: var(--color-badge-red-text); }
  .chip[data-hue='purple'] { --_bg: var(--color-badge-purple-bg); --_fg: var(--color-badge-purple-text); }
  .chip[data-hue='teal']   { --_bg: var(--color-badge-teal-bg);   --_fg: var(--color-badge-teal-text); }
  .chip[data-hue='orange'] { --_bg: var(--color-badge-orange-bg); --_fg: var(--color-badge-orange-text); }

  /* sizes: sm 20px / base 24px / lg 28px row height */
  .chip[data-size='small'] {
    padding: 5px 10px;
    border-radius: 10px;
    font-size: 10px;
    line-height: 14px;
  }
  .chip[data-size='small'] :global(.icon) { font-size: 10px; }
  .chip[data-size='large'] {
    padding: 7px 14px;
    border-radius: 14px;
    font-size: var(--text-base);
    line-height: 20px;
  }
  .chip[data-size='large'] :global(.icon) { font-size: var(--text-base); }

  /* icon-only: geometry scales with size */
  .chip.icon-only { padding: 3px; border-radius: 8px; }
  .chip.icon-only[data-size='small'] { padding: 2px; border-radius: 6px; }
  .chip.icon-only[data-size='large'] { padding: 4px; border-radius: 12px; }
</style>
