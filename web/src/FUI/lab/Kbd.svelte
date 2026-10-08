<script lang="ts">
// Kbd: keycap chip. Props: keys string[], size.
  import { squircle } from '../core/squircle';
  let { keys, size = 'medium' } = $props<{ keys: string[]; size?: 'small' | 'medium' | 'large' }>();
</script>
<span class="kbd" data-size={size} use:squircle>
  {#each keys as k, i (i)}<kbd>{k}</kbd>{#if i < keys.length - 1}<span class="plus" aria-hidden="true">+</span>{/if}{/each}
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .kbd { display: inline-flex; align-items: center; gap: var(--fui-space-1); background: var(--fui-elev); border-radius: var(--fui-radius-xs); padding: var(--fui-space-1) var(--fui-space-2); }
  .kbd kbd { font-family: var(--fui-font-mono); font-size: var(--fui-text-xs); color: var(--fui-color-text); }
  .kbd[data-size='small'] kbd { font-size: var(--fui-text-xs); }
  .kbd[data-size='large'] kbd { font-size: var(--fui-text-base); }
  .plus { color: var(--fui-color-text-disabled); font-size: var(--fui-text-xs); }
</style>
