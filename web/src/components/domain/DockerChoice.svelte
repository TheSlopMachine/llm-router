<script lang="ts">
  import { Text } from '@fuckyouryan/fui'

  let {
    title,
    hint,
    selected,
    disabled = false,
    onclick,
  }: {
    title: string
    hint: string
    selected: boolean
    disabled?: boolean
    onclick: () => void
  } = $props()
</script>

<button type="button" class="docker-choice" class:selected {disabled} {onclick}>
  <Text weight="medium">{title}</Text>
  <Text size="sm" foregroundStyle="secondary">{hint}</Text>
</button>

<style>
  .docker-choice {
    all: unset;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-1);
    padding: var(--fui-space-3) var(--fui-space-4);
    border-radius: var(--fui-radius-md);
    background: var(--fui-color-surface-container);
    outline: 2px solid transparent;
  }
  .docker-choice:focus-visible {
    outline-color: var(--fui-color-accent);
  }
  .docker-choice.selected {
    outline-color: var(--fui-color-accent);
  }
  .docker-choice:disabled {
    cursor: not-allowed;
    opacity: 0.6;
  }
</style>
