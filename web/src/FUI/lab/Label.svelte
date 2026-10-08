<script lang="ts">
// Label: icon + text pair. Props: icon, text, size, tone, iconPosition.
  import Icon from '../controls/Icon.svelte';
  let { icon = '', text, size = 'base', tone = 'default', iconPosition = 'start' } = $props<{
    icon?: string; text: string; size?: 'xs'|'sm'|'base'|'md'|'lg'; tone?: 'default'|'soft'|'disabled'|'accent'|'danger'|'success'|'warning'; iconPosition?: 'start'|'end';
  }>();
</script>
<span class="label" class:end={iconPosition === 'end'}>
  {#if icon && iconPosition === 'start'}<Icon name={icon} {size} {tone} />{/if}
  <span class="t t-{size} t-{tone ?? 'default'}">{text}</span>
  {#if icon && iconPosition === 'end'}<Icon name={icon} {size} {tone} />{/if}
</span>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .label { display: inline-flex; align-items: center; gap: var(--fui-space-2); min-width: 0; }
  .label.end { flex-direction: row; }
  .t { font-family: var(--fui-font-normal); line-height: var(--fui-leading-base); }
  .t-xs { font-size: var(--fui-text-xs); } .t-sm { font-size: var(--fui-text-sm); }
  .t-base { font-size: var(--fui-text-base); } .t-md { font-size: var(--fui-text-md); } .t-lg { font-size: var(--fui-text-lg); }
  .t-default { color: var(--fui-color-text); } .t-soft { color: var(--fui-color-text-soft); }
  .t-disabled { color: var(--fui-color-text-disabled); } .t-accent { color: var(--fui-color-accent); }
  .t-danger { color: var(--fui-color-error-text); } .t-success { color: var(--fui-color-success-text); } .t-warning { color: var(--fui-color-warning-text); }
</style>
