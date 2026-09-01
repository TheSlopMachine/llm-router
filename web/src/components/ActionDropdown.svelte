<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import { cubicOut, cubicIn } from 'svelte/easing'

  export let actions: Array<{ id: string; label: string; icon?: string; disabled?: boolean }> = []
  export let label: string = 'Actions'
  export let disabled: boolean = false
  export let rounded: 'sm' | 'lg' = 'sm'

  const dispatch = createEventDispatcher<{ action: string }>()

  let isOpen = false
  let dropdownElement: HTMLDivElement
  let triggerElement: HTMLButtonElement
  let shouldFlipUp = false

  function toggle() {
    if (disabled) return
    isOpen = !isOpen
    if (isOpen) checkFlipPosition()
  }

  function onAction(id: string, actDisabled?: boolean) {
    if (actDisabled) return
    dispatch('action', id)
    isOpen = false
  }

  function checkFlipPosition() {
    if (!triggerElement) return
    const rect = triggerElement.getBoundingClientRect()
    const spaceBelow = window.innerHeight - rect.bottom
    const spaceAbove = rect.top
    const estimatedHeight = actions.length * 40 + 8
    shouldFlipUp = spaceBelow < Math.min(estimatedHeight, 180) && spaceAbove > spaceBelow
  }

  function handleClickOutside(e: MouseEvent) {
    if (isOpen && dropdownElement && !dropdownElement.contains(e.target as Node)) {
      isOpen = false
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && isOpen) {
      e.preventDefault()
      isOpen = false
      triggerElement?.focus()
    }
  }

  onMount(() => {
    document.addEventListener('click', handleClickOutside)
  })
  onDestroy(() => {
    document.removeEventListener('click', handleClickOutside)
  })
</script>

<div class="dropdown autoWidth" class:disabled bind:this={dropdownElement}>
  <button
    class="dropdown-trigger"
    class:open={isOpen}
    class:rounded-lg={rounded === 'lg'}
    bind:this={triggerElement}
    on:click|stopPropagation={toggle}
    on:keydown={handleKeydown}
    {disabled}
    aria-haspopup="menu"
    aria-expanded={isOpen}
    type="button"
  >
    <span class="dropdown-label">{label}</span>
    <span class="icon chevron" class:open={isOpen}>expand_more</span>
  </button>

  {#if isOpen}
    <div
      class="dropdown-menu"
      class:flip-up={shouldFlipUp}
      role="menu"
      in:fly={{ y: shouldFlipUp ? 8 : -8, duration: 200, easing: cubicOut, opacity: 0 }}
      out:fade={{ duration: 150, easing: cubicIn }}
    >
      <div class="dropdown-options">
        {#each actions as act}
          <button
            class="dropdown-option"
            on:click={() => onAction(act.id, act.disabled)}
            disabled={act.disabled}
            role="menuitem"
          >
            {#if act.icon}<span class="icon" style="font-size:16px;margin-right:8px">{act.icon}</span>{/if}
            {act.label}
          </button>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .dropdown {
    position: relative;
    width: 100%;
  }
  .dropdown.autoWidth {
    width: auto;
    display: inline-flex;
  }
  .dropdown.autoWidth .dropdown-trigger {
    width: auto;
    gap: 8px;
  }
  .dropdown.autoWidth .dropdown-label {
    flex: 0 1 auto;
  }
  .dropdown.autoWidth .dropdown-menu {
    min-width: 100%;
    width: max-content;
    max-width: 320px;
    right: auto;
  }
  .dropdown.disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }
  .dropdown-trigger {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 6px 12px;
    font-family: inherit;
    font-size: 14px;
    font-weight: 400;
    border-radius: 8px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface);
    color: var(--color-text);
    cursor: pointer;
    transition: border-color 0.15s ease;
    text-align: left;
  }

  .dropdown-trigger.rounded-lg {
    border-radius: 12px;
  }
  .dropdown-trigger:hover:not(:disabled) { border-color: var(--color-text-soft); }
  .dropdown-trigger:focus { border-color: var(--color-text-soft); outline: none; }
  .dropdown-trigger:disabled {
    background-color: var(--color-surface-container);
    color: var(--color-text-disabled);
    border-color: var(--color-outline-soft);
    cursor: not-allowed;
  }
  .dropdown-label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chevron {
    font-size: 20px;
    color: var(--color-text-soft);
    transition: transform 200ms cubic-bezier(0.4, 0, 0.2, 1);
    flex-shrink: 0;
  }
  .chevron.open { transform: rotate(180deg); }
  .dropdown-menu {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    right: 0;
    z-index: 1000;
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
    box-shadow: var(--shadow-lg);
    overflow: hidden;
  }
  .dropdown-menu.flip-up { top: auto; bottom: calc(100% + 4px); }
  .dropdown-options {
    max-height: 180px;
    overflow-y: auto;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .dropdown-option {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    width: 100%;
    padding: 8px 12px;
    font-size: 14px;
    text-align: left;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text);
    cursor: pointer;
    transition: none !important;
  }
  .dropdown-option:hover:not(:disabled) { background: var(--color-nav-hover); }
  .dropdown-option:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
