<script lang="ts">
  import { toast } from '../lib/toast.svelte'
  import type { ToastItem } from '../lib/toast.svelte'
  import { squircle } from '../lib/squircle'

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
      <span class="icon">{item.kind === 'success' ? 'check_circle' : 'error'}</span>
      <span class="toast-text">{item.text}</span>
      <button
        class="btn-icon toast-copy"
        onclick={() => void copy(item)}
        aria-label="Copy to clipboard"
        title="Copy"
        use:squircle={8}
      >
        <span class="icon">{copiedId === item.id ? 'check' : 'content_copy'}</span>
      </button>
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
    gap: 8px;
  }
  .toast {
    display: flex;
    align-items: flex-start;
    justify-content: flex-start;
    gap: 10px;
    width: fit-content;
    /* grows with content up to 1.5x the base width, then wraps in height */
    min-width: 320px;
    max-width: min(480px, calc(100vw - 32px));
    /* ...and up to a third of the viewport in height, then scrolls */
    max-height: 33vh;
    padding: 12px 16px;
    border-radius: var(--radius-md);
    background: var(--color-surface-container-highest);
    color: var(--color-text);
    font-size: 14px;
    text-align: left;
    animation: toast-in 0.28s cubic-bezier(0.3, 1.15, 0.5, 1);
  }
  @keyframes toast-in {
    from {
      opacity: 0;
      transform: translateX(24px) scale(0.96);
    }
  }
  .toast .icon {
    font-size: 20px;
    flex-shrink: 0;
  }
  .toast-success > .icon {
    color: var(--color-success-text);
  }
  .toast-error > .icon {
    color: var(--color-error-text);
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
    padding-right: 4px;
  }
  /* Copy affordance: revealed on hover (or keyboard focus), never a
     whole-toast click target. */
  .toast-copy {
    opacity: 0;
    padding: 4px;
    transition: opacity 0.15s ease;
    flex-shrink: 0;
  }
  .toast-copy .icon {
    font-size: 16px;
    line-height: 1;
  }
  .toast:hover .toast-copy,
  .toast:focus-within .toast-copy {
    opacity: 1;
  }
</style>
