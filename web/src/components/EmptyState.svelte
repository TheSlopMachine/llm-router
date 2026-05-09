<script lang="ts">
  export let icon: string
  export let message: string
  export let hint: string
  export let buttonText: string
  export let buttonIcon: string
  export let onButtonClick: () => void
</script>

<div class="empty">
  <span class="icon empty-icon">{icon}</span>
  <p>{message}</p>
  <p class="empty-hint">{hint}</p>
  <button class="btn btn-primary" on:click={onButtonClick}>
    <span class="icon">{buttonIcon}</span>
    {buttonText}
  </button>
</div>

<style>
  .empty {
    text-align: center;
    padding: 64px 32px;
  }

  .empty .empty-icon {
    font-size: 64px;
    color: var(--color-text-soft);
    display: block;
    margin-bottom: 16px;
  }

  .empty p {
    margin: 8px 0;
    color: var(--color-text);
    font-size: 16px;
  }

  .empty-hint {
    color: var(--color-text-soft);
    font-size: 14px;
    margin-bottom: 32px;
  }
</style>
