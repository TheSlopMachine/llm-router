<script lang="ts">
  import { onDestroy } from 'svelte'
  import { modal } from '../lib/modal'
  import type { ModalConfig, ModalButton } from '../lib/modal'

  let modalStack: ModalConfig[] = []
  
  const unsubscribe = modal.subscribe(state => {
    modalStack = state.stack
    
    if (modalStack.length > 0) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
  })
  
  onDestroy(() => {
    unsubscribe()
    document.body.style.overflow = ''
  })
  
  function handleBackdropClick(config: ModalConfig): void {
    if (config.severity === 'low') {
      modal.close()
    }
  }
  
  function handleKeyDown(event: KeyboardEvent, config: ModalConfig, modalElement: HTMLElement): void {
    if (event.key === 'Escape' && config.severity !== 'high') {
      modal.close()
      return
    }
    
    if (event.key === 'Tab') {
      trapFocus(event, modalElement)
    }
  }
  
  function trapFocus(event: KeyboardEvent, modalElement: HTMLElement): void {
    const focusable = modalElement.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
    )
    
    if (focusable.length === 0) return
    
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    
    if (event.shiftKey) {
      if (document.activeElement === first) {
        last.focus()
        event.preventDefault()
      }
    } else {
      if (document.activeElement === last) {
        first.focus()
        event.preventDefault()
      }
    }
  }
  
  function getContentProps(config: ModalConfig): Record<string, any> {
    if (config.type !== 'content') return {}
    
    return {
      ...config.props,
      updateButtons: (buttons: ModalButton[]) => {
        modal.updateButtons(buttons)
      },
      updateTitle: (title: string) => {
        modal.updateTitle(title)
      },
      closeModal: () => {
        modal.close()
      }
    }
  }
  
  function handleCloseButton(config: ModalConfig): void {
    if (config.severity !== 'high') {
      modal.close()
    }
  }
</script>

{#each modalStack as config, index (index)}
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div 
    class="modal-backdrop" 
    style="z-index: {1000 + index * 2}"
    on:click={() => handleBackdropClick(config)}
    role="presentation">
    
    <div 
      class="modal-card modal-{config.size}" 
      style="z-index: {1001 + index * 2}"
      on:click|stopPropagation
      on:keydown={(e) => handleKeyDown(e, config, e.currentTarget)}
      role="dialog"
      aria-modal="true"
      aria-labelledby="modal-title-{index}"
      tabindex="-1">
      
      <div class="modal-header">
        <h2 id="modal-title-{index}">{config.title}</h2>
        {#if config.severity !== 'high'}
          <button 
            class="close-btn" 
            on:click={() => handleCloseButton(config)}
            aria-label="Close modal">
            <span class="icon">close</span>
          </button>
        {/if}
      </div>
      
      <div class="modal-body">
        {#if config.type === 'confirm'}
          <p>{config.message}</p>
        {:else if config.content}
          <svelte:component 
            this={config.content} 
            {...getContentProps(config)} />
        {/if}
      </div>
      
      {#if config.buttons && config.buttons.length > 0}
        <div class="modal-footer">
          {#each config.buttons as button}
            <button 
              class="btn btn-{button.variant || 'secondary'}"
              on:click={button.onClick}
              disabled={button.disabled || button.loading}>
              {button.loading ? 'Loading...' : button.label}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/each}

<style>
  .modal-backdrop {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: transparent;
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    animation: fadeIn 0.15s ease-out;
  }
  
  .modal-card {
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 16px;
    box-shadow: var(--shadow-lg);
    max-height: 90vh;
    display: flex;
    flex-direction: column;
    animation: scaleIn 0.15s ease-out;
  }
  
  .modal-small { width: 90%; max-width: 400px; }
  .modal-medium { width: 90%; max-width: 600px; }
  .modal-large { width: 90%; max-width: 800px; }
  .modal-extra-large { width: 90%; max-width: 1100px; }
  
  .modal-header {
    padding: 20px 24px;
    border-bottom: 1px solid var(--color-outline-soft);
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-shrink: 0;
    gap: 16px;
  }
  
  .modal-header h2 {
    font-size: 18px;
    font-weight: 500;
    color: var(--color-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }
  
  .close-btn {
    padding: 4px;
    border: none;
    background: transparent;
    cursor: pointer;
    color: var(--color-text-soft);
    border-radius: 4px;
    transition: background 0.15s, color 0.15s;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  
  .close-btn:hover {
    background: var(--color-hover-bg);
    color: var(--color-text);
  }
  
  .close-btn .icon {
    font-size: 20px;
  }
  
  .modal-body {
    padding: 24px;
    overflow-y: auto;
    flex: 1;
    min-height: 0;
  }
  
  .modal-body p {
    color: var(--color-text);
    line-height: 1.5;
    margin: 0;
  }
  
  .modal-footer {
    padding: 16px 24px;
    border-top: 1px solid var(--color-outline-soft);
    display: flex;
    justify-content: flex-end;
    gap: 12px;
    flex-shrink: 0;
  }
  
  @keyframes fadeIn {
    from { opacity: 0; }
    to { opacity: 1; }
  }
  
  @keyframes scaleIn {
    from { 
      transform: scale(0.95); 
      opacity: 0; 
    }
    to { 
      transform: scale(1); 
      opacity: 1; 
    }
  }
</style>
