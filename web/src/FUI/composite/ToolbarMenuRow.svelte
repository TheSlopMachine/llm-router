<script lang="ts">
  import Text from '../controls/Text.svelte'
  import { setToolbarPlacement, type ToolbarItemReg } from './toolbar.svelte'
  let { item } = $props<{ item: ToolbarItemReg }>()
  setToolbarPlacement('menu')
</script>

<div class="row">
  {#if item.label}<span class="lbl"><Text size="sm">{item.label}</Text></span>{/if}
  {@render item.children()}
</div>

<style>
  .row { display: flex; align-items: center; gap: var(--fui-space-3); min-width: 0; }
  .lbl { flex: 1 1 auto; min-width: 0; }
</style>
