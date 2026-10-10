<script lang="ts">
// Field: wrapper label + control + hint/error. Props: label, hint, error, required, orientation, children.
  import type { Snippet } from 'svelte';
  let { label, hint = '', error = '', required = false, orientation = 'vertical', children } = $props<{
    label: string; hint?: string; error?: string; required?: boolean;
    orientation?: 'vertical' | 'horizontal'; children: Snippet;
  }>();
  const msg = $derived(error || hint);
</script>
<div class="field" class:hor={orientation === 'horizontal'}>
  <span class="lab">{label}{#if required}<span class="req" aria-hidden="true">*</span>{/if}</span>
  <div class="ctl">{@render children()}</div>
  {#if msg}<span class="msg" class:err={!!error} role={error ? 'alert' : undefined}>{msg}</span>{/if}
</div>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .field { display: flex; flex-direction: column; gap: var(--fui-space-2); min-width: 0; }
  .field.hor { flex-direction: row; align-items: center; gap: var(--fui-space-4); }
  .field.hor .ctl { flex: 1; }
  .lab { font-family: var(--fui-font-normal); font-size: var(--fui-text-sm); font-weight: var(--fui-weight-medium); color: var(--fui-color-text); }
  .req { color: var(--fui-color-error-text); }
  .ctl { min-width: 0; }
  .msg { font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); line-height: var(--fui-leading-base); }
  .msg.err { color: var(--fui-color-error-text); }
</style>
