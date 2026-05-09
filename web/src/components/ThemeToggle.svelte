<script lang="ts">
  import { theme } from '../lib/theme'
  
  let currentTheme: 'auto' | 'light' | 'dark' = 'auto'
  
  theme.subscribe(value => {
    currentTheme = value
  })
  
  function cycleTheme() {
    theme.cycle()
  }
  
  $: icon = currentTheme === 'auto' ? 'brightness_auto'
           : currentTheme === 'light' ? 'light_mode'
           : 'dark_mode'
  
  $: label = currentTheme === 'auto' ? 'Auto'
            : currentTheme === 'light' ? 'Light'
            : 'Dark'
</script>

<button 
  class="theme-toggle" 
  on:click={cycleTheme} 
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
