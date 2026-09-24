<script lang="ts">
  import Button from './ui/controls/Button.svelte'

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


  .empty .empty-icon {
    font-size: 64px;
    color: var(--color-text-soft);
    display: block;
    margin-bottom: var(--space-5);
  }

  .empty p {
    margin: var(--space-3) 0;
    color: var(--color-text);
    font-size: var(--text-md);
  }

</style>
