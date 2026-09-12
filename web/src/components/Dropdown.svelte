<script lang="ts">
  import { fly, fade } from 'svelte/transition'
  import { cubicOut, cubicIn } from 'svelte/easing'
  import { untrack } from 'svelte'
  import { portal } from '../lib/portal'

  type SelectOption = { value: string; label: string }
  type Action = { id: string; label: string; icon?: string; disabled?: boolean; danger?: boolean }

  let {
    value = $bindable(''),
    options = [],
    actions = [],
    placeholder = 'Select...',
    label = 'Actions',
    disabled = false,
    searchable = false,
    searchThreshold = 10,
    autoWidth = false,
    rounded = 'sm',
    triggerIcon = '',
    onchange,
    onaction
  } = $props<{
    value?: string
    options?: SelectOption[]
    actions?: Action[]
    placeholder?: string
    label?: string
    disabled?: boolean
    searchable?: boolean
    searchThreshold?: number
    autoWidth?: boolean
    rounded?: 'sm' | 'lg'
    triggerIcon?: string
    onchange?: (v: string) => void
    onaction?: (id: string) => void
  }>()

  let isActionMode = $derived(actions.length > 0)
  let effectiveAutoWidth = $derived(autoWidth || isActionMode)

  let isOpen = $state(false)
  let searchQuery = $state('')
  let highlightedIndex = $state(-1)
  let dropdownElement = $state<HTMLDivElement>()
  let triggerElement = $state<HTMLButtonElement>()
  let menuElement = $state<HTMLDivElement>()
  let shouldFlipUp = $state(false)
  let menuTop = $state(0)
  let menuLeft = $state(0)
  let menuWidth = $state(0)

  let selectedOption = $derived(options.find((opt: SelectOption) => opt.value === value))
  let selectedLabel = $derived(selectedOption?.label || placeholder)
  let showSearch = $derived(searchable || options.length > searchThreshold)
  let filteredOptions = $derived(
    searchQuery
      ? options.filter((opt: SelectOption) => opt.label.toLowerCase().includes(searchQuery.toLowerCase()))
      : options
  )

  function toggle() {
    if (disabled) return
    isOpen = !isOpen
    if (isOpen) {
      positionMenu()
      if (!isActionMode) {
        highlightedIndex = options.findIndex((opt: SelectOption) => opt.value === value)
      }
    } else {
      searchQuery = ''
    }
  }

  function select(optionValue: string) {
    value = optionValue
    isOpen = false
    searchQuery = ''
    onchange?.(optionValue)
    triggerElement?.focus()
  }

  function handleAction(id: string, actDisabled?: boolean) {
    if (actDisabled) return
    onaction?.(id)
    isOpen = false
  }

  function estimateMenuHeight(): number {
    return isActionMode
      ? Math.min(actions.length * 40 + 8, 180)
      : Math.min(filteredOptions.length * 40 + (showSearch ? 50 : 0), 180)
  }

  // positionMenu pins the portaled menu to the trigger with fixed
  // coordinates, so overflow:hidden ancestors cannot clip it.
  // Horizontal clamp keeps the menu inside the viewport.
  function positionMenu(): void {
    if (!triggerElement) return
    const rect = triggerElement.getBoundingClientRect()
    const estimatedHeight = estimateMenuHeight()
    const estimatedWidth = Math.max(rect.width, 200)
    const spaceBelow = window.innerHeight - rect.bottom
    const spaceAbove = rect.top
    shouldFlipUp = spaceBelow < estimatedHeight && spaceAbove > spaceBelow
    menuTop = shouldFlipUp ? Math.max(8, rect.top - estimatedHeight - 4) : rect.bottom + 4
    const rightAligned = rect.left + estimatedWidth > window.innerWidth - 8
    menuLeft = rightAligned
      ? Math.max(8, rect.right - estimatedWidth)
      : Math.max(8, rect.left)
    menuWidth = Math.max(1, Math.round(rect.width))
  }

  function handleKeydown(e: KeyboardEvent) {
    if (isActionMode) {
      if (e.key === 'Escape' && isOpen) {
        e.preventDefault()
        e.stopPropagation()
        isOpen = false
        triggerElement?.focus()
      }
      return
    }
    if (disabled) return
    switch (e.key) {
      case 'Enter':
      case ' ':
        if (!isOpen) {
          e.preventDefault()
          toggle()
        } else if (highlightedIndex >= 0 && highlightedIndex < filteredOptions.length) {
          e.preventDefault()
          select(filteredOptions[highlightedIndex].value)
        }
        break
      case 'Escape':
        if (isOpen) {
          e.preventDefault()
          isOpen = false
          searchQuery = ''
          triggerElement?.focus()
        }
        break
      case 'ArrowDown':
        e.preventDefault()
        if (!isOpen) {
          toggle()
        } else {
          highlightedIndex = Math.min(highlightedIndex + 1, filteredOptions.length - 1)
        }
        break
      case 'ArrowUp':
        e.preventDefault()
        if (isOpen) {
          highlightedIndex = Math.max(highlightedIndex - 1, 0)
        }
        break
      case 'Home':
        if (isOpen) {
          e.preventDefault()
          highlightedIndex = 0
        }
        break
      case 'End':
        if (isOpen) {
          e.preventDefault()
          highlightedIndex = filteredOptions.length - 1
        }
        break
    }
  }

  $effect(() => {
    function isMenuOpen(): boolean {
      return untrack(() => isOpen)
    }
    function handleClickOutside(e: MouseEvent): void {
      const target = e.target as Node
      if (
        isMenuOpen() &&
        dropdownElement &&
        menuElement &&
        !dropdownElement.contains(target) &&
        !menuElement.contains(target)
      ) {
        isOpen = false
        searchQuery = ''
      }
    }
    function handleReposition(): void {
      if (isMenuOpen()) positionMenu()
    }
    document.addEventListener('click', handleClickOutside)
    window.addEventListener('resize', handleReposition)
    window.addEventListener('scroll', handleReposition, true)
    return () => {
      document.removeEventListener('click', handleClickOutside)
      window.removeEventListener('resize', handleReposition)
      window.removeEventListener('scroll', handleReposition, true)
    }
  })
</script>

<div class="dropdown" class:disabled class:autoWidth={effectiveAutoWidth} bind:this={dropdownElement}>
  <button
    class="dropdown-trigger"
    class:open={isOpen}
    class:rounded-lg={rounded === 'lg'}
    bind:this={triggerElement}
    onclick={isActionMode ? (e) => { e.stopPropagation(); toggle() } : toggle}
    onkeydown={handleKeydown}
    {disabled}
    role={isActionMode ? undefined : 'combobox'}
    aria-haspopup={isActionMode ? 'menu' : 'listbox'}
    aria-expanded={isOpen}
    aria-label={isActionMode ? label : undefined}
    aria-controls={isActionMode ? undefined : 'dropdown-menu'}
    type="button"
  >
    {#if triggerIcon}
      <span class="icon trigger-icon">{triggerIcon}</span>
    {:else}
      <span class="dropdown-label">{isActionMode ? label : selectedLabel}</span>
      <span class="icon chevron" class:open={isOpen}>expand_more</span>
    {/if}
  </button>

  {#if isOpen}
    {@const menuStyle = effectiveAutoWidth
      ? `top: ${menuTop}px; left: ${menuLeft}px; min-width: ${menuWidth}px;`
      : `top: ${menuTop}px; left: ${menuLeft}px; width: ${menuWidth}px;`}
    <div
      use:portal
      bind:this={menuElement}
      class="dropdown-menu"
      class:full-width={!effectiveAutoWidth}
      style={menuStyle}
      id={isActionMode ? undefined : 'dropdown-menu'}
      role={isActionMode ? 'menu' : 'listbox'}
      in:fly={{ y: shouldFlipUp ? 8 : -8, duration: 200, easing: cubicOut, opacity: 0 }}
      out:fade={{ duration: 150, easing: cubicIn }}
    >
      {#if isActionMode}
        <div class="dropdown-options">
          {#each actions as act}
            <button
              class="dropdown-option"
              class:danger={act.danger}
              onclick={() => handleAction(act.id, act.disabled)}
              disabled={act.disabled}
              role="menuitem"
            >
              {#if act.icon}<span class="icon" style="font-size:16px;margin-right:8px">{act.icon}</span>{/if}
              {act.label}
            </button>
          {/each}
        </div>
      {:else}
        {#if showSearch}
          <div class="dropdown-search">
            <input
              type="text"
              placeholder="Search..."
              bind:value={searchQuery}
              onclick={(e) => e.stopPropagation()}
              onkeydown={(e) => e.stopPropagation()}
            />
          </div>
        {/if}
        <div class="dropdown-options">
          {#each filteredOptions as option, i}
            <button
              class="dropdown-option"
              class:selected={option.value === value}
              class:highlighted={i === highlightedIndex}
              onclick={() => select(option.value)}
              role="option"
              aria-selected={option.value === value}
            >
              {option.label}
            </button>
          {:else}
            <div class="dropdown-empty">No options found</div>
          {/each}
        </div>
      {/if}
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
    border-radius: var(--radius-md);
    border: none;
    background: var(--color-surface-container-highest);
    color: var(--color-text);
    cursor: pointer;
    transition: background-color 0.15s ease;
    text-align: left;
  }

  .dropdown-trigger.rounded-lg {
    border-radius: var(--radius-lg);
  }

  .dropdown-trigger:hover:not(:disabled) {
    background: var(--color-outline-light);
  }

  .dropdown-trigger:focus {
    outline: none;
    box-shadow: inset 0 0 0 2px var(--color-accent);
  }

  .trigger-icon {
    font-size: 20px;
    margin: 0 auto;
  }

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

  .chevron.open {
    transform: rotate(180deg);
  }

  .dropdown-menu {
    position: fixed;
    z-index: 2000;
    width: max-content;
    max-width: min(320px, calc(100vw - 16px));
    background: var(--color-surface-container-high);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    overflow: hidden;
  }

  .dropdown-menu.full-width {
    width: auto;
  }

  .dropdown-search {
    padding: 8px;
    border-bottom: 1px solid var(--color-outline-light);
  }

  .dropdown-search input {
    width: 100%;
    padding: 6px 12px;
    font-size: 14px;
    border: 1px solid var(--color-outline-light);
    border-radius: 6px;
    outline: none;
  }

  .dropdown-search input:focus {
    border-color: var(--color-text-soft);
  }

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

  .dropdown-option:hover,
  .dropdown-option.highlighted {
    background: var(--color-nav-hover);
  }

  .dropdown-option.selected {
    background: var(--color-nav-active);
    font-weight: 500;
  }

  .dropdown-option:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .dropdown-option.danger {
    color: var(--color-danger, #b42318);
  }

  .dropdown-empty {
    padding: 12px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }
</style>
