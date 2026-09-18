<script lang="ts">
  import Button from './ui/Button.svelte'

  let { icon, message, hint = '', buttonText, buttonIcon, onButtonClick } = $props<{
    icon: string
    message: string
    hint?: string
    buttonText: string
    buttonIcon: string
    onButtonClick: () => void
  }>()
</script>

<div class="empty">
  <span class="icon empty-icon">{icon}</span>
  <p>{message}</p>
  {#if hint}<p class="empty-hint">{hint}</p>{/if}
  <Button text={buttonText} style="prominent" icon={{ name: buttonIcon }} onclick={onButtonClick} />
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
