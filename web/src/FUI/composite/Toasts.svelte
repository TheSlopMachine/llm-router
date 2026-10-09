<script lang="ts">
  import Icon from '../controls/Icon.svelte'
  import { toast } from '../core/toast.svelte'
  import type { ToastItem } from '../core/toast.svelte'
  import { squircle } from '../core/squircle'
  import Button from '../controls/Button.svelte'

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
      use:squircle
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
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .toast-stack {
    position: fixed;
    top: var(--fui-toast-offset);
    right: var(--fui-toast-offset);
    z-index: var(--fui-z-toast);
    display: flex;
    flex-direction: column;
    /* right edge anchored: each toast takes the width its text needs */
    align-items: flex-end;
    gap: var(--fui-space-3);
  }
  .toast {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: var(--fui-space-4);
    width: fit-content;
    /* grows with content up to 1.5x the base width, then wraps in height */
    min-width: var(--fui-toast-min-w);
    max-width: min(var(--fui-popover-w-lg), calc(100vw - var(--fui-toast-max-h-reduce) - var(--fui-space-3)));
    /* ...and up to a third of the viewport in height, then scrolls */
    max-height: 33vh;
    padding: var(--fui-space-4) var(--fui-space-5);
    border-radius: var(--fui-radius-md);
    background: var(--fui-color-surface-container-highest);
    color: var(--fui-color-text);
    font-size: var(--fui-text-base);
    text-align: left;
    animation: toast-in var(--fui-dur-slow) var(--fui-ease-spring);
  }
  @keyframes toast-in {
    from {
      opacity: 0;
      transform: translateX(var(--fui-toast-shift)) scale(0.96);
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
    max-height: calc(33vh - var(--fui-toast-max-h-reduce));
    overflow-y: auto;
    scrollbar-gutter: stable;
    padding-right: var(--fui-space-2);
  }
  /* Actions fade in on hover/focus; the wrapper owns the motion so the
     Buttons stay plain library Buttons. */
  .toast-actions {
    display: flex;
    gap: var(--fui-space-2);
    flex-shrink: 0;
    opacity: 0;
    transition: opacity var(--fui-dur-base) ease;
  }
  .toast:hover .toast-actions,
  .toast:focus-within .toast-actions {
    opacity: 1;
  }
</style>
