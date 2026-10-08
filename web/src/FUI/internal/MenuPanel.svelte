<script lang="ts">
  // Internal: the anchored, portaled surface shared by Select (listbox) and
  // FloatingList (action menu). Owns the surface look, the options column and
  // the enter/leave motion; the callers own semantics (role, keyboard, items).
  // The {#if} lives here on purpose: Svelte transitions are local, so they
  // only play when the block that directly contains them toggles.
  import type { Snippet } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import { cubicOut, cubicIn } from 'svelte/easing'
  import { portal } from '../core/portal'
  import { squircle } from '../core/squircle'
  import { resolveMenuWidth } from '../core/steps'

  let {
    open,
    el = $bindable(),
    top,
    left,
    width,
    minWidth,
    flipped = false,
    role,
    label,
    id,
    onkeydown,
    header,
    children,
  } = $props<{
    open: boolean
    el?: HTMLDivElement | undefined
    top: number
    left: number
    /** fixed width (px); mutually exclusive with minWidth */
    width?: number
    minWidth?: number
    flipped?: boolean
    role: 'listbox' | 'menu'
    label?: string
    id?: string
    onkeydown?: (e: KeyboardEvent) => void
    /** rendered above the options column (Select search box) */
    header?: Snippet
    children: Snippet
  }>()

  const size = $derived(resolveMenuWidth(width, minWidth))
</script>

{#if open}
  <!-- tabindex only for role=menu (FloatingList focuses it for Escape); a listbox keeps focus on its trigger -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    use:portal
    bind:this={el}
    class="panel"
    style="top: {top}px; left: {left}px; {size}"
    {id}
    {role}
    aria-label={label}
    tabindex={role === 'menu' ? -1 : undefined}
    {onkeydown}
    use:squircle={12}
    in:fly={{ y: flipped ? 8 : -8, duration: 200, easing: cubicOut, opacity: 0 }}
    out:fade={{ duration: 150, easing: cubicIn }}
  >
    {@render header?.()}
    <div class="options">
      {@render children()}
    </div>
  </div>
{/if}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .panel {
    position: fixed;
    z-index: 2000;
    width: max-content;
    max-width: min(480px, calc(100vw - 16px));
    /* Overlay surface: solid like toasts, never the translucent --elev wash. */
    background: var(--fui-color-surface-container-highest);
    /* borderless by squircle design: borders do not follow the clip */
    border: none;
    border-radius: var(--fui-radius-md);
    overflow: hidden;
  }
  .options {
    max-height: 180px;
    overflow-y: auto;
    padding: var(--fui-space-2);
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-2);
  }
</style>
