<script lang="ts">
  import { fly, fade } from 'svelte/transition'
  import { cubicOut, cubicIn } from 'svelte/easing'
  import { untrack } from 'svelte'

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
  let shouldFlipUp = $state(false)

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
      checkFlipPosition()
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

  function checkFlipPosition() {
    if (!triggerElement) return
    const rect = triggerElement.getBoundingClientRect()
    const spaceBelow = window.innerHeight - rect.bottom
    const spaceAbove = rect.top
    const estimatedHeight = isActionMode
      ? Math.min(actions.length * 40 + 8, 180)
      : Math.min(filteredOptions.length * 40 + (showSearch ? 50 : 0), 180)
    shouldFlipUp = spaceBelow < estimatedHeight && spaceAbove > spaceBelow
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
    function handleClickOutside(e: MouseEvent) {
      if (untrack(() => isOpen) && dropdownElement && !dropdownElement.contains(e.target as Node)) {
        isOpen = false
        searchQuery = ''
      }
    }
    document.addEventListener('click', handleClickOutside)
    return () => document.removeEventListener('click', handleClickOutside)
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
    aria-controls={isActionMode ? undefined : 'dropdown-menu'}
    type="button"
  >
    <span class="dropdown-label">{isActionMode ? label : selectedLabel}</span>
    <span class="icon chevron" class:open={isOpen}>expand_more</span>
  </button>

  {#if isOpen}
    <div
      class="dropdown-menu"
      class:flip-up={shouldFlipUp}
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

  .dropdown-trigger:hover:not(:disabled) {
    border-color: var(--color-text-soft);
  }

  .dropdown-trigger:focus {
    border-color: var(--color-text-soft);
    outline: none;
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

  .dropdown-menu.flip-up {
    top: auto;
    bottom: calc(100% + 4px);
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
