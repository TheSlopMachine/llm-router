<script lang="ts">
  import { toast } from '../lib/toast.svelte'
  import { squircle } from '../lib/squircle'
</script>

<div class="toast-stack" aria-live="polite">
  {#each toast.items as item (item.id)}
    <button
      class="toast {item.kind === 'success' ? 'toast-success' : 'toast-error'}"
      onclick={() => toast.dismiss(item.id)}
      use:squircle={12}
    >
      <span class="icon">{item.kind === 'success' ? 'check_circle' : 'error'}</span>
      <span class="toast-text">{item.text}</span>
    </button>
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
    gap: 8px;
    max-width: min(420px, calc(100vw - 32px));
  }
  .toast {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    width: 100%;
    max-width: 100%;
    padding: 12px 16px;
    border: none;
    border-radius: var(--radius-md);
    background: var(--color-surface-container-highest);
    color: var(--color-text);
    font-family: inherit;
    font-size: 14px;
    text-align: left;
    cursor: pointer;
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
  .toast-success .icon {
    color: var(--color-success-text);
  }
  .toast-error .icon {
    color: var(--color-error-text);
  }
  .toast-text {
    min-width: 0;
    overflow-wrap: anywhere;
    word-break: break-word;
  }
</style>
