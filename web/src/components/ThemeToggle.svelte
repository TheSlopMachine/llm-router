<script lang="ts">
  import { theme } from '../lib/theme.svelte'

  let icon = $derived(
    theme.value === 'auto'
      ? 'brightness_auto'
      : theme.value === 'light'
        ? 'light_mode'
        : 'dark_mode'
  )

  let label = $derived(
    theme.value === 'auto' ? 'Auto' : theme.value === 'light' ? 'Light' : 'Dark'
  )

  function cycleTheme() {
    theme.cycle()
  }
</script>

<button
  class="theme-toggle"
  onclick={cycleTheme}
  title="Theme: {label} (click to cycle)"
  aria-label="Toggle theme"
>
  <span class="icon">{icon}</span>
  <span class="theme-label">{label}</span>
</button>

<style>
  .theme-toggle {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: 8px;
    width: 100%;
    text-align: left;
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text-soft);
    background: transparent;
    border: none;
    cursor: pointer;
  }

  .theme-toggle:hover {
    background: var(--color-nav-hover);
    color: var(--color-text);
  }

  .theme-toggle .icon {
    font-size: 20px;
  }

  .theme-label {
    font-size: 14px;
  }
</style>
