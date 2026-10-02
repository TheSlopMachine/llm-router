<script lang="ts">
  import Icon from './ui/controls/Icon.svelte'
  import { toast } from '../lib/toast.svelte'
  import type { ToastItem } from '../lib/toast.svelte'
  import { squircle } from '../lib/squircle'
  import Button from './ui/controls/Button.svelte'

  let copiedId = $state(0)
  let copyTimer: ReturnType<typeof setTimeout> | undefined = undefined

  async function copy(item: ToastItem): Promise<void> {
    try {
      await navigator.clipboard.writeText(item.text)
    } catch {
      toast.error('Could not copy to clipboard')
      return
    }
    copiedId = item.id
    if (copyTimer !== undefined) clearTimeout(copyTimer)
    copyTimer = setTimeout(() => {
      copiedId = 0
    }, 1500)
  }
</script>

<div class="toast-stack" aria-live="polite">
  {#each toast.items as item (item.id)}
    <div
      class="toast {item.kind === 'success' ? 'toast-success' : 'toast-error'}"
      role="status"
      onmouseenter={() => toast.pause(item.id)}
      onmouseleave={() => toast.resume(item.id)}
      use:squircle={12}
    >
      <span class="toast-icon">
        <Icon name={item.kind === 'success' ? 'check_circle' : 'error'} size="lg" tone={item.kind === 'success' ? 'success' : 'danger'} />
      </span>
      <span class="toast-text">{item.text}</span>
      <div class="toast-actions">
        <Button
          size="small"
          style="text"
          icon={{ name: copiedId === item.id ? 'check' : 'content_copy' }}
          ariaLabel="Copy to clipboard"
          title="Copy"
          onclick={() => void copy(item)}
        />
        <Button
          size="small"
          style="text"
          icon={{ name: 'close' }}
          ariaLabel="Dismiss"
          title="Dismiss"
          onclick={() => toast.dismiss(item.id)}
        />
      </div>
    </div>
  {/each}
</div>

<style>
  .toast-stack {
    position: fixed;
    top: 16px;
    right: 16px;
    z-index: 100;
    display: flex;
    flex-direction: column;
    /* right edge anchored: each toast takes the width its text needs */
    align-items: flex-end;
    gap: var(--space-3);
  }
  .toast {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: var(--space-4);
    width: fit-content;
    /* grows with content up to 1.5x the base width, then wraps in height */
    min-width: 320px;
    max-width: min(480px, calc(100vw - 32px));
    /* ...and up to a third of the viewport in height, then scrolls */
    max-height: 33vh;
    padding: var(--space-4) var(--space-5);
    border-radius: var(--radius-md);
    background: var(--color-surface-container-highest);
    color: var(--color-text);
    font-size: var(--text-base);
    text-align: left;
    animation: toast-in 0.28s cubic-bezier(0.3, 1.15, 0.5, 1);
  }
  @keyframes toast-in {
    from {
      opacity: 0;
      transform: translateX(24px) scale(0.96);
    }
  }
  .toast-icon {
    display: inline-flex;
    flex-shrink: 0;
  }
  .toast-text {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
    word-break: break-word;
    /* the icon stays pinned while overshoot text scrolls inside;
       the gutter keeps the scrollbar off the glyphs */
    max-height: calc(33vh - 24px);
    overflow-y: auto;
    scrollbar-gutter: stable;
    padding-right: var(--space-2);
  }
  /* Actions fade in on hover/focus; the wrapper owns the motion so the
     Buttons stay plain library Buttons. */
  .toast-actions {
    display: flex;
    gap: var(--space-2);
    flex-shrink: 0;
    opacity: 0;
    transition: opacity 0.15s ease;
  }
  .toast:hover .toast-actions,
  .toast:focus-within .toast-actions {
    opacity: 1;
  }
</style>
