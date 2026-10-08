<script lang="ts">
// GroupBox: surface with title + content. Props: title, padding, children.
  import type { Snippet } from 'svelte';
  import { squircle } from '../core/squircle';
  let { title = '', padding = 4, children } = $props<{ title?: string; padding?: 0|1|2|3|4|5|6|7|8; children: Snippet }>();
  const PAD: Record<number,string> = { 0:'var(--fui-space-0)',1:'var(--fui-space-1)',2:'var(--fui-space-2)',3:'var(--fui-space-3)',4:'var(--fui-space-4)',5:'var(--fui-space-5)',6:'var(--fui-space-6)',7:'var(--fui-space-7)',8:'var(--fui-space-8)' };
</script>
<section class="box" use:squircle style:padding={PAD[padding]}>
  {#if title}<h3 class="title">{title}</h3>{/if}
  <div class="body">{@render children()}</div>
</section>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .box { background: var(--fui-elev); border-radius: var(--fui-radius-md); }
  .title { font-family: var(--fui-font-normal); font-size: var(--fui-text-sm); font-weight: var(--fui-weight-medium); color: var(--fui-color-text-soft); margin-bottom: var(--fui-space-3); }
  .body { min-width: 0; }
</style>
