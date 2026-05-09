<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import { cubicOut, cubicIn } from 'svelte/easing'

  export let value: string = ''
  export let options: Array<{value: string, label: string}> = []
  export let placeholder: string = 'Select...'
  export let disabled: boolean = false
  export let searchable: boolean = false
  export let searchThreshold: number = 10

  let isOpen = false
  let searchQuery = ''
  let highlightedIndex = -1
  let dropdownElement: HTMLDivElement
  let triggerElement: HTMLButtonElement
  let shouldFlipUp = false

  const dispatch = createEventDispatcher<{ change: string }>()

  $: selectedOption = options.find(opt => opt.value === value)
  $: selectedLabel = selectedOption?.label || placeholder
  $: showSearch = searchable || (options.length > searchThreshold)
  $: filteredOptions = searchQuery 
    ? options.filter(opt => opt.label.toLowerCase().includes(searchQuery.toLowerCase()))
    : options

  function toggle() {
    if (disabled) return
    isOpen = !isOpen
    if (isOpen) {
      checkFlipPosition()
      highlightedIndex = options.findIndex(opt => opt.value === value)
    } else {
      searchQuery = ''
    }
  }

  function select(optionValue: string) {
    value = optionValue
    isOpen = false
    searchQuery = ''
    dispatch('change', optionValue)
    triggerElement?.focus()
  }

  function checkFlipPosition() {
    if (!triggerElement) return
    const rect = triggerElement.getBoundingClientRect()
    const spaceBelow = window.innerHeight - rect.bottom
    const spaceAbove = rect.top
    const estimatedHeight = Math.min(filteredOptions.length * 40 + (showSearch ? 50 : 0), 180)
    
    shouldFlipUp = spaceBelow < estimatedHeight && spaceAbove > spaceBelow
  }

  function handleKeydown(e: KeyboardEvent) {
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

  function handleClickOutside(e: MouseEvent) {
    if (isOpen && dropdownElement && !dropdownElement.contains(e.target as Node)) {
      isOpen = false
      searchQuery = ''
    }
  }

  onMount(() => {
    document.addEventListener('click', handleClickOutside)
  })

  onDestroy(() => {
    document.removeEventListener('click', handleClickOutside)
  })
</script>

<div class="dropdown" class:disabled bind:this={dropdownElement}>
  <button
    class="dropdown-trigger"
    class:open={isOpen}
    bind:this={triggerElement}
    on:click={toggle}
    on:keydown={handleKeydown}
    {disabled}
    role="combobox"
    aria-expanded={isOpen}
    aria-haspopup="listbox"
    aria-controls="dropdown-menu"
  >
    <span class="dropdown-label">{selectedLabel}</span>
    <span class="icon chevron" class:open={isOpen}>expand_more</span>
  </button>

  {#if isOpen}
    <div
      class="dropdown-menu"
      class:flip-up={shouldFlipUp}
      id="dropdown-menu"
      role="listbox"
      in:fly={{y: shouldFlipUp ? 8 : -8, duration: 200, easing: cubicOut, opacity: 0}}
      out:fade={{duration: 150, easing: cubicIn}}
    >
      {#if showSearch}
        <div class="dropdown-search">
          <input
            type="text"
            placeholder="Search..."
            bind:value={searchQuery}
            on:click|stopPropagation
            on:keydown|stopPropagation
          />
        </div>
      {/if}

      <div class="dropdown-options">
        {#each filteredOptions as option, i}
          <button
            class="dropdown-option"
            class:selected={option.value === value}
            class:highlighted={i === highlightedIndex}
            on:click={() => select(option.value)}
            role="option"
            aria-selected={option.value === value}
          >
            {option.label}
          </button>
        {:else}
          <div class="dropdown-empty">No options found</div>
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
    display: block;
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

  .dropdown-empty {
    padding: 12px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }
</style>
