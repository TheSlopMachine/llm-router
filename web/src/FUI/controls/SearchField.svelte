<script lang="ts">
  import TextEdit from './TextEdit.svelte'
  import Button from './Button.svelte'
  import { fillStyle, type FillSize } from '../tokens'
  import { getToolbarCtx, getToolbarPinned } from '../composite/toolbar.svelte'

  // Collapses to a button only while it does not fit next to its neighbours
  // (Toolbar level >= 2, not pinned) and only while empty. The close icon first clears
  // the text, then (when collapsible) folds the field back into the button.
  let {
    value = $bindable(''),
    placeholder = 'Search...',
    disabled = false,
    collapsed,
    fill,
  } = $props<{
    value?: string
    placeholder?: string
    disabled?: boolean
    /** force collapsible on/off; default: follows the surrounding Toolbar */
    collapsed?: boolean
    fill?: FillSize
  }>()

  const tb = getToolbarCtx()
  const pinned = getToolbarPinned()
  const collapsible = $derived(collapsed ?? (!!tb && !pinned && tb.level >= 2))

  let open = $state(false)
  let root = $state<HTMLElement>()
  const compact = $derived(collapsible && !open && value === '')

  $effect(() => {
    if (!collapsible) open = false
  })

  function focusInput(): void {
    queueMicrotask(() => root?.querySelector('input')?.focus())
  }
  function openSearch(): void {
    open = true
    focusInput()
  }
  function handleClear(): void {
    if (value !== '') {
      value = ''
      focusInput()
    } else if (collapsible) {
      open = false
    }
  }
  function handleKey(e: KeyboardEvent): void {
    if (e.key !== 'Escape') return
    e.stopPropagation()
    handleClear()
  }
</script>

{#if compact}
  <Button
    icon={{ name: 'search' }}
    text={placeholder}
    collapse={false}
    ariaExpanded={false}
    {disabled}
    onclick={openSearch}
  />
{:else}
  <div class="sfld" class:anim={open} bind:this={root} style:flex={fillStyle(fill)} style:min-width={fill ? '0' : undefined}>
    <span class="sfld-hint" aria-hidden="true">{placeholder}</span>
    <div class="sfld-field">
      <TextEdit
        bind:value
        hint={placeholder}
        {disabled}
        leadingIcon="search"
        trailing={[{ icon: 'close', label: 'Clear', onclick: handleClear }]}
        onkeydown={handleKey}
      />
    </div>
  </div>
{/if}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  /* Width = hint text (+ room for the icons), capped by the row; the input itself
     does not size the track (width: 0 + min-width: 100%). */
  .sfld { display: grid; min-width: 0; max-width: 100%; }
  .sfld > * { grid-area: 1 / 1; }
  .sfld-hint {
    visibility: hidden;
    white-space: nowrap;
    height: 0;
    overflow: hidden;
    padding-inline: calc(var(--fui-ctl-medium) * 2);
  }
  .sfld-field { width: 0; min-width: 100%; }
  .sfld.anim { animation: sfld-in var(--fui-dur-base) ease both; }
  @keyframes sfld-in { from { opacity: 0; max-width: 0; } to { opacity: 1; max-width: 100%; } }
  @media (prefers-reduced-motion: reduce) { .sfld.anim { animation: none; } }
</style>
