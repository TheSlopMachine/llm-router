<script lang="ts">
  import { modal } from '../lib/modal.svelte'
  import type { ModalConfig, ModalButton, ModalMenu } from '../lib/modal.svelte'
  import ActionDropdown from './ActionDropdown.svelte'

  let stack = $derived(modal.stack)

  $effect(() => {
    document.body.style.overflow = stack.length ? 'hidden' : ''
    return () => {
      document.body.style.overflow = ''
    }
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
      updateMenu: (menu: ModalMenu | null) => {
        modal.updateMenu(menu)
      },
      updateTitle: (title: string) => {
        modal.updateTitle(title)
      },
      updateSubtitle: (subtitle: string) => {
        modal.updateSubtitle(subtitle)
      },
      updateStepper: (stepper: import('../lib/modal.svelte').StepperConfig | null) => {
        modal.updateStepper(stepper)
      },
      updateFooterHint: (hint: string) => {
        modal.updateFooterHint(hint)
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

{#each stack as config, index (index)}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="modal-backdrop"
    style="z-index: {1000 + index * 2}"
    onclick={() => handleBackdropClick(config)}
    role="presentation">

    <div
      class="modal-card modal-{config.size}"
      style="z-index: {1001 + index * 2}"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => handleKeyDown(e, config, e.currentTarget as HTMLElement)}
      role="dialog"
      aria-modal="true"
      aria-labelledby="modal-title-{index}"
      tabindex="-1">

      <div class="modal-header">
        <div class="modal-title-col">
          <h2 id="modal-title-{index}">{config.title}</h2>
          {#if config.subtitle}
            <p class="modal-subtitle">{config.subtitle}</p>
          {/if}
        </div>
        {#if config.severity !== 'high'}
          <button
            class="close-btn"
            onclick={() => handleCloseButton(config)}
            aria-label="Close modal">
            <span class="icon">close</span>
          </button>
        {/if}
      </div>

      {#if config.stepper}
        <div class="modal-stepper">
          <div class="stepper-track">
            <div class="stepper-track-fill" style="width: {((config.stepper.current - 1) / Math.max(1, config.stepper.total - 1)) * 100}%"></div>
          </div>
          <div class="stepper-steps">
            {#each config.stepper.labels as label, i}
              {@const n = i + 1}
              {@const isCompleted = n < config.stepper.current}
              {@const isCurrent = n === config.stepper.current}
              <div class="stepper-step" class:completed={isCompleted} class:current={isCurrent}>
                <div class="step-circle">
                  {#if isCompleted}
                    <span class="icon" style="font-size: 16px;">check</span>
                  {:else}
                    {n}
                  {/if}
                </div>
                <span class="step-label">{label}</span>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      <div class="modal-body">
        {#if config.type === 'confirm'}
          <p>{config.message}</p>
        {:else if config.type === 'content'}
          {@const Content = config.content}
          {#if Content}
            <Content {...getContentProps(config)} />
          {/if}
        {/if}
      </div>

      {#if (config.buttons && config.buttons.length > 0) || config.menu || config.footerHint}
        <div class="modal-footer">
          {#if config.footerHint}
            <span class="footer-hint">{config.footerHint}</span>
          {/if}
          {#if (config.buttons && config.buttons.length > 0) || config.menu}
            <div class="footer-actions">
              {#if config.buttons}
                {#each config.buttons as button}
                  <button
                    class="btn btn-{button.variant || 'secondary'}"
                    onclick={button.onClick}
                    disabled={button.disabled || button.loading}>
                    {button.loading ? 'Loading...' : button.label}
                  </button>
                {/each}
              {/if}
              {#if config.menu}
                <ActionDropdown label={config.menu.label} actions={config.menu.actions} onaction={config.menu.onaction} />
              {/if}
            </div>
          {/if}
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
    align-items: flex-start;
    flex-shrink: 0;
    gap: 16px;
  }

  .modal-title-col {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }

  .modal-header h2 {
    font-size: 18px;
    font-weight: 500;
    color: var(--color-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .modal-subtitle {
    font-size: 12px;
    font-weight: 400;
    color: var(--color-text-soft);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    line-height: 16px;
  }

  .modal-stepper {
    padding: 16px 24px 0 24px;
    flex-shrink: 0;
    position: relative;
  }

  .stepper-track {
    position: absolute;
    top: 28px;
    left: 40px;
    right: 40px;
    height: 2px;
    background: var(--color-outline-light);
    border-radius: 9999px;
  }

  .stepper-track-fill {
    height: 100%;
    background: var(--color-text-soft);
    border-radius: 9999px;
    transition: width 0.25s ease;
  }

  .stepper-steps {
    display: flex;
    justify-content: space-between;
    position: relative;
  }

  .stepper-step {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    flex: 1;
  }

  .step-circle {
    width: 24px;
    height: 24px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 12px;
    font-weight: 600;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface);
    color: var(--color-text-soft);
    position: relative;
    z-index: 1;
  }

  .stepper-step.current .step-circle {
    background: var(--color-text);
    border-color: var(--color-text);
    color: var(--color-surface);
  }

  .stepper-step.completed .step-circle {
    background: var(--color-text);
    border-color: var(--color-text);
    color: var(--color-surface);
  }

  .step-label {
    font-size: 11px;
    font-weight: 500;
    color: var(--color-text-soft);
    text-align: center;
    line-height: 14px;
  }

  .stepper-step.current .step-label {
    color: var(--color-text);
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
    justify-content: space-between;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
  }

  .footer-hint {
    font-size: 12px;
    color: var(--color-error-text);
    line-height: 16px;
    flex: 1;
    min-width: 0;
  }

  .footer-actions {
    display: flex;
    gap: 12px;
    flex-shrink: 0;
    margin-left: auto;
    flex-wrap: wrap;
    justify-content: flex-end;
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
