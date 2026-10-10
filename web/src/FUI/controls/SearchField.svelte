<script lang="ts">
  import { tick } from 'svelte'
  import TextEdit from './TextEdit.svelte'
  import Button from './Button.svelte'
  import { fillStyle, type FillSize } from '../tokens'
  import { getToolbarCtx, getToolbarPinned } from '../composite/toolbar.svelte'

  // Collapsible search. Where it does not fit (Toolbar level >= 2, not pinned) it is a prominent
  // "magnifier + hint" button that MORPHS into the text field: one element, its width animates
  // between the two natural widths while the faces cross-fade. The close icon first clears the
  // text, then folds the field back. A non-empty field never folds.
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

  let expanded = $state(false)
  let root = $state<HTMLElement>()
  let animating = $state(false)
  const showField = $derived(!collapsible || expanded || value !== '')

  $effect(() => {
    if (!collapsible) expanded = false
  })

  function durationMs(): number {
    if (!root) return 0
    const raw = getComputedStyle(root).getPropertyValue('--fui-dur-slow').trim()
    const n = parseFloat(raw)
    if (!Number.isFinite(n)) return 0
    return raw.endsWith('ms') ? n : n * 1000
  }

  // Measure the width before and after the state flip, then animate between them.
  async function morph(next: boolean): Promise<void> {
    if (!root || expanded === next) {
      expanded = next
      return
    }
    const w0 = root.offsetWidth
    expanded = next
    await tick()
    const w1 = root.offsetWidth
    const ms = durationMs()
    if (next) root.querySelector('input')?.focus({ preventScroll: true })
    if (w0 === w1 || ms === 0 || matchMedia('(prefers-reduced-motion: reduce)').matches) return
    animating = true
    const a = root.animate(
      [
        { width: `${w0}px`, flexBasis: `${w0}px`, flexGrow: 0, flexShrink: 0 },
        { width: `${w1}px`, flexBasis: `${w1}px`, flexGrow: 0, flexShrink: 0 },
      ],
      { duration: ms, easing: 'ease' },
    )
    await a.finished.catch(() => {})
    animating = false
  }

  function focusInput(): void {
    queueMicrotask(() => root?.querySelector('input')?.focus({ preventScroll: true }))
  }
  function handleClear(): void {
    if (value !== '') {
      value = ''
      focusInput()
    } else if (collapsible) {
      void morph(false)
    }
  }
  function handleKey(e: KeyboardEvent): void {
    if (e.key !== 'Escape') return
    e.stopPropagation()
    handleClear()
  }
</script>

{#snippet fieldBody()}
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
{/snippet}

{#if collapsible}
  <div
    class="sfm"
    class:show-field={showField}
    class:animating
    bind:this={root}
    style:flex={showField ? fillStyle(fill) : undefined}
  >
    <div class="face face-btn" inert={showField} aria-hidden={showField}>
      <Button
        block
        icon={{ name: 'search' }}
        text={placeholder}
        collapse={false}
        ariaExpanded={showField}
        {disabled}
        style="prominent"
        onclick={() => void morph(true)}
      />
    </div>
    <div class="face face-field sfld" inert={!showField} aria-hidden={!showField}>
      {@render fieldBody()}
    </div>
  </div>
{:else}
  <div class="sfld" bind:this={root} style:flex={fillStyle(fill)} style:min-width={fill ? '0' : undefined}>
    {@render fieldBody()}
  </div>
{/if}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  /* Field width = hint text (+ room for icons), capped by the row; the input itself does not
     size the track (width: 0 + min-width: 100%). */
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

  .sfm { position: relative; display: grid; min-width: 0; max-width: 100%; }
  .sfm.animating { overflow: hidden; }
  .face {
    grid-area: 1 / 1;
    width: 100%;
    min-width: 0;
    transition: opacity var(--fui-dur-fast) ease;
  }
  /* the hidden face leaves the flow, so the root is sized by the visible one */
  .sfm:not(.show-field) .face-field,
  .sfm.show-field .face-btn {
    position: absolute;
    inset: 0;
    opacity: 0;
    pointer-events: none;
  }
</style>
