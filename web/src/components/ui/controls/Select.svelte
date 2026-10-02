<script lang="ts">
  // Listbox select. Split out of the old Dropdown god-component, which drove
  // a listbox and an action menu from one `isActionMode` branch: two ARIA
  // roles, two keyboard models and two sets of props in one 470-line file.
  import { untrack } from 'svelte'
  import { squircle } from '../../../lib/squircle'
  import { anchorTo, bindDismiss, MENU_ROW_HEIGHT, MENU_MAX_HEIGHT } from '../../../lib/popover'
  import { t } from '../../../lib/i18n.svelte'
  import type { Size } from '../tokens'
  import Icon from './Icon.svelte'
  import MenuPanel from '../internal/MenuPanel.svelte'
  import MenuItem from '../internal/MenuItem.svelte'

  export type SelectOption = { value: string; label: string }

  let {
    value = $bindable(''),
    options = [],
    placeholder = 'Select...',
    disabled = false,
    searchable = false,
    searchThreshold = 10,
    autoWidth = false,
    rounded = 'sm',
    size = 'medium',
    ariaLabel,
    onchange,
  } = $props<{
    value?: string
    options?: SelectOption[]
    placeholder?: string
    disabled?: boolean
    searchable?: boolean
    searchThreshold?: number
    autoWidth?: boolean
    rounded?: 'sm' | 'lg'
    size?: Size
    ariaLabel?: string
    onchange?: (v: string) => void
  }>()

  // aria-controls needs a stable unique id per instance: several selects can
  // be open in one modal, and a shared literal id would cross-wire them.
  const menuId = `select-menu-${Math.random().toString(36).slice(2, 9)}`

  let isOpen = $state(false)
  let searchQuery = $state('')
  let highlightedIndex = $state(-1)
  let rootElement = $state<HTMLDivElement>()
  let triggerElement = $state<HTMLButtonElement>()
  let menuElement = $state<HTMLDivElement>()
  let flipped = $state(false)
  let menuTop = $state(0)
  let menuLeft = $state(0)
  let menuWidth = $state(0)

  const selectedOption = $derived(options.find((o: SelectOption) => o.value === value))
  const selectedLabel = $derived(selectedOption?.label || placeholder)
  const showSearch = $derived(searchable || options.length > searchThreshold)
  const filteredOptions = $derived(
    searchQuery
      ? options.filter((o: SelectOption) => o.label.toLowerCase().includes(searchQuery.toLowerCase()))
      : options
  )

  function position(): void {
    if (!triggerElement) return
    const height = Math.min(
      filteredOptions.length * MENU_ROW_HEIGHT + (showSearch ? 50 : 0),
      MENU_MAX_HEIGHT
    )
    const r = anchorTo(triggerElement, { height })
    menuTop = r.top
    menuLeft = r.left
    menuWidth = r.width
    flipped = r.flipped
  }

  function toggle(): void {
    if (disabled) return
    isOpen = !isOpen
    if (isOpen) {
      position()
      highlightedIndex = options.findIndex((o: SelectOption) => o.value === value)
    } else {
      searchQuery = ''
    }
  }

  function select(optionValue: string): void {
    value = optionValue
    isOpen = false
    searchQuery = ''
    onchange?.(optionValue)
    triggerElement?.focus()
  }

  function handleKeydown(e: KeyboardEvent): void {
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
        if (!isOpen) toggle()
        else highlightedIndex = Math.min(highlightedIndex + 1, filteredOptions.length - 1)
        break
      case 'ArrowUp':
        if (isOpen) {
          e.preventDefault()
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

  $effect(() => 
    bindDismiss({
      isOpen: () => untrack(() => isOpen),
      contains: (target) =>
        Boolean(rootElement?.contains(target)) || Boolean(menuElement?.contains(target)),
      onDismiss: () => {
        isOpen = false
        searchQuery = ''
      },
      onReposition: position,
    })
  )
</script>

<div class="dropdown" class:disabled class:autoWidth bind:this={rootElement}>
  <button
    class="trigger"
    data-size={size}
    class:open={isOpen}
    class:rounded-lg={rounded === 'lg'}
    bind:this={triggerElement}
    onclick={toggle}
    onkeydown={handleKeydown}
    {disabled}
    type="button"
    role="combobox"
    aria-haspopup="listbox"
    aria-expanded={isOpen}
    aria-controls={menuId}
    aria-label={ariaLabel}
    use:squircle={12}
  >
    <span class="label">{selectedLabel}</span>
    <span class="chevron" class:open={isOpen}><Icon name="expand_more" size="lg" tone="soft" /></span>
  </button>

  <MenuPanel
    open={isOpen}
    bind:el={menuElement}
    top={menuTop}
    left={menuLeft}
    width={autoWidth ? undefined : menuWidth}
    minWidth={autoWidth ? menuWidth : undefined}
    {flipped}
    role="listbox"
    id={menuId}
  >
    {#snippet header()}
      {#if showSearch}
        <div class="search">
          <input
            type="text"
            placeholder={t('Search...')}
            bind:value={searchQuery}
            onclick={(e) => e.stopPropagation()}
            onkeydown={(e) => e.stopPropagation()}
            use:squircle={12}
          />
        </div>
      {/if}
    {/snippet}
    {#each filteredOptions as option (option.value)}
      <MenuItem
        role="option"
        selected={option.value === value}
        highlighted={filteredOptions.indexOf(option) === highlightedIndex}
        onclick={() => select(option.value)}
      >
        {option.label}
      </MenuItem>
    {:else}
      <div class="empty">{t('No options found')}</div>
    {/each}
  </MenuPanel>
</div>

<style>
  /* Everything the trigger looks like lives here. The menu surface and rows
     live in internal/MenuPanel + MenuItem, shared with FloatingList. */
  .dropdown {
    position: relative;
    width: 100%;
  }

  .dropdown.autoWidth {
    width: auto;
    display: inline-flex;
  }

  .dropdown.autoWidth .trigger {
    width: auto;
    /* autoWidth changes width only: padding, fill and type stay identical to
       the full-width trigger. The gap only keeps the chevron off the label
       once space-between has no free space to distribute. */
    gap: var(--space-3);
  }

  .dropdown.disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .trigger {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    height: var(--ctl-medium);
    padding: 6px var(--field-pad-h);
    font-family: inherit;
    font-size: var(--text-base);
    line-height: 20px;
    font-weight: 400;
    border-radius: var(--ctl-radius);
    border: none;
    background: var(--elev);
    color: var(--color-text);
    cursor: pointer;
    transition: background-color 0.15s ease;
    text-align: left;
  }
  .trigger[data-size='small'] {
    height: var(--ctl-small);
    padding-top: 2px;
    padding-bottom: 2px;
  }
  .trigger[data-size='large'] {
    height: var(--ctl-large);
    padding-top: 10px;
    padding-bottom: 10px;
  }
  .trigger.rounded-lg {
    border-radius: var(--radius-lg);
  }

  .trigger:hover:not(:disabled) {
    background: var(--color-outline-light);
  }
  /* The menu opening is the feedback: no press bounce on a wide trigger. */
  .trigger:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }
  .trigger:disabled {
    background-color: var(--color-surface-container);
    color: var(--color-text-disabled);
    cursor: not-allowed;
  }

  .label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chevron {
    display: inline-flex;
    transition: transform 200ms cubic-bezier(0.4, 0, 0.2, 1);
    flex-shrink: 0;
  }
  .chevron.open {
    transform: rotate(180deg);
  }

  .search {
    padding: var(--space-3);
    border-bottom: 1px solid var(--color-outline-light);
  }
  /* Compact field: same fill/ring system as TextEdit, tighter padding. */
  .search input {
    width: 100%;
    padding: 6px var(--field-pad-h);
    font: inherit;
    line-height: 20px;
    color: var(--color-text);
    background: var(--elev);
    border: 1px solid transparent;
    border-radius: var(--ctl-radius);
    outline: none;
  }
  .search input:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }

  .empty {
    padding: var(--space-4);
    text-align: center;
    color: var(--color-text-soft);
    font-size: var(--text-base);
  }
</style>
