<script lang="ts">
  import { modal } from '../core/modal.svelte'
  import type { ModalConfig, ModalButton, ModalMenu, StepperConfig } from '../core/modal.svelte'
  import { fuiText } from '../core/texts'
  import { squircle } from '../core/squircle'
  import Button from '../controls/Button.svelte'
  import Text from '../controls/Text.svelte'
  import FloatingList from '../controls/FloatingList.svelte'
  import StepsView from './StepsView.svelte'
  import { hasFooterButtons, isTextButtonVariant } from '../core/ui-guards'

  let stack = $derived(modal.stack)

  let footerMenuOpen = $state(false)
  let footerMenuAnchor = $state<HTMLElement>()

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

  function footerStyle(variant: ModalButton['variant']): 'prominent' | 'none' | 'text' {
    if (variant === 'primary') return 'prominent'
    if (variant === 'secondary') return 'text'
    if (variant && isTextButtonVariant(variant)) return 'text'
    return variant === 'danger' ? 'prominent' : 'none'
  }

  function footerTint(variant: ModalButton['variant']): string | undefined {
    return variant === 'danger' || variant === 'text danger' ? 'var(--fui-color-danger)' : undefined
  }

  function hasBody(config: ModalConfig): boolean {
    if (config.type === 'confirm') return !!(config.content || config.message)
    if (config.type === 'content') return !!config.content
    return !!config.contentSnippet
  }

  function hasFooter(config: ModalConfig): boolean {
    return hasFooterButtons(config.buttons) || !!config.menu
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
      updateStepper: (stepper: StepperConfig | null) => {
        modal.updateStepper(stepper)
      },
      closeModal: () => {
        modal.close()
      }
    }
  }
</script>

{#each stack as config, index (config)}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="modal-backdrop"
    style="z-index: {1000 + index * 2}"
    onclick={() => handleBackdropClick(config)}
    role="presentation">

    <div
      class="modal-card modal-{config.size} {config.className || ''}"
      class:modal-confirm={config.type === 'confirm'}
      style="z-index: {1001 + index * 2}"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => handleKeyDown(e, config, e.currentTarget as HTMLElement)}
      role="dialog"
      aria-modal="true"
      aria-labelledby={config.title ? `modal-title-${index}` : undefined}
      tabindex="-1"
      use:squircle>

      {#if config.title || config.type !== 'confirm'}
        <div class="modal-header">
          <div class="modal-title-col">
            {#if config.title}
              <Text variant="section-title" id="modal-title-{index}" truncate>{config.title}</Text>
            {/if}
            {#if config.subtitle}
              <div class="modal-subtitle"><Text size="sm" tone="soft" truncate>{config.subtitle}</Text></div>
            {/if}
          </div>
          {#if config.type !== 'confirm'}
            <div class="modal-close">
              <Button icon={{ name: 'close' }} ariaLabel={fuiText('close')} onclick={() => modal.close()} />
            </div>
          {/if}
        </div>
      {/if}

      {#if config.stepper}
        <div class="modal-stepper">
          <StepsView steps={config.stepper.labels} current={config.stepper.current} />
        </div>
      {/if}

      {#if config.type === 'snippet'}
        <div class="modal-body">
          {@render config.contentSnippet()}
        </div>
      {:else if hasBody(config)}
        <div class="modal-body">
          {#if config.type === 'confirm'}
            {#if config.content}
              {@const ConfirmContent = config.content}
              <ConfirmContent {...(config.props ?? {})} />
            {:else}
              <Text tag="p">{config.message}</Text>
            {/if}
          {:else if config.type === 'content'}
            {@const Content = config.content}
            {#if Content}
              <Content {...getContentProps(config)} />
            {/if}
          {/if}
        </div>
      {/if}

      {#if hasFooter(config)}
        <div class="modal-footer" class:footer-bordered={config.type === 'content'}>
          <div class="footer-actions">
            {#if config.buttons}
              {#each config.buttons as button}
                <Button
                  style={footerStyle(button.variant)}
                  tint={footerTint(button.variant)}
                  onclick={() => void button.onClick()}
                  disabled={button.disabled || button.loading}>
                  {button.loading ? 'Loading...' : button.label}
                </Button>
              {/each}
            {/if}
            {#if config.menu}
              <Button
                text={config.menu.label}
                icon={{ name: 'expand_more', placement: 'right' }}
                onclick={(e) => {
                  footerMenuAnchor = e.currentTarget as HTMLElement
                  footerMenuOpen = !footerMenuOpen
                }}
              />
              <FloatingList
                bind:open={footerMenuOpen}
                anchor={footerMenuAnchor}
                label={config.menu.label}
                actions={config.menu.actions}
                onaction={config.menu.onaction}
              />
            {/if}
          </div>
        </div>
      {/if}
    </div>
  </div>
{/each}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .modal-backdrop {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: transparent;
    backdrop-filter: blur(var(--fui-modal-blur));
    display: flex;
    align-items: center;
    justify-content: center;
    animation: fadeIn var(--fui-dur-base) ease-out;
  }

  .modal-card {
    background: var(--fui-color-surface-container-high);
    border: none;
    border-radius: var(--fui-radius-lg);
    max-height: 90vh;
    max-height: 90dvh;
    display: flex;
    flex-direction: column;
    animation: scaleIn var(--fui-dur-base) ease-out;
  }

  .modal-small { width: min(var(--fui-modal-w-sm), calc(100% - 2 * var(--fui-space-3))); max-width: var(--fui-modal-w-sm); }
  .modal-medium { width: min(var(--fui-modal-w-md), calc(100% - 2 * var(--fui-space-3))); max-width: var(--fui-modal-w-md); }
  .modal-large { width: min(var(--fui-modal-w-lg), calc(100% - 2 * var(--fui-space-3))); max-width: var(--fui-modal-w-lg); }
  .modal-extra-large { width: min(var(--fui-modal-w-xl), calc(100% - 2 * var(--fui-space-3))); max-width: var(--fui-modal-w-xl); }

  .modal-header {
    padding: var(--fui-modal-pad-header) var(--fui-modal-pad-header) 0;
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    flex-shrink: 0;
    gap: var(--fui-space-5);
  }

  /* Hover fill lands 14px from the corner, same as the footer buttons.
     The offset lives on a wrapper this component owns, not on a class
     pushed into Button (which needed :global and leaked app-wide). */
  .modal-close {
    flex-shrink: 0;
    margin: calc(var(--fui-modal-close-offset) * -1) calc(var(--fui-modal-close-offset) * -1) 0 0;
  }

  .modal-title-col {
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-1);
    flex: 1;
    min-width: 0;
  }

  .modal-subtitle {
    line-height: var(--fui-modal-subtitle-leading);
  }

  .modal-stepper {
    padding: var(--fui-modal-pad-body) var(--fui-modal-pad-body) 0 var(--fui-modal-pad-body);
    flex-shrink: 0;
  }

  .modal-body {
    padding: var(--fui-modal-pad-body);
    overflow-y: auto;
    flex: 1;
    min-height: 0;
  }

  /* One spacing rhythm N=12: edge-to-last-button, buttons-to-bottom edge,
     and between adjacent buttons are all the same N. */
  .modal-footer {
    padding: var(--fui-modal-pad-body);
    display: flex;
    justify-content: flex-end;
    align-items: center;
    gap: var(--fui-space-4);
    flex-shrink: 0;
  }

  .modal-footer.footer-bordered {
    border-top: var(--fui-border-w) solid var(--fui-color-outline-soft);
  }

  .footer-actions {
    display: flex;
    gap: var(--fui-space-4);
    flex-shrink: 0;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  /* Confirm specifics: larger message text, tighter button pair. */
  .modal-card.modal-confirm {
    max-width: var(--fui-modal-confirm-w);
  }

  /* Confirm message is fill-less text: it sits on the 20px header grid,
     not on the 14px content grid. */
  .modal-confirm .modal-body {
    padding: var(--fui-modal-pad-body) var(--fui-modal-confirm-pad-x);
  }

  /* Title-less confirm: the message is the first text — 20px off the top. */
  .modal-confirm .modal-body:first-child {
    padding-top: var(--fui-modal-pad-header);
  }

  .modal-confirm .footer-actions {
    gap: var(--fui-space-2);
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
