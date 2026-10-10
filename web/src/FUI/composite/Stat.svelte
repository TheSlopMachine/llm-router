<script lang="ts">
  import type { Snippet } from 'svelte'
  import Text from '../controls/Text.svelte'
  import Icon from '../controls/Icon.svelte'

  // label over value; optional trend (delta), hint and a sparkline slot.
  // delta > 0 is "good" unless invert (errors going up is bad news).
  let { label, value, tone, delta, invert = false, hint, sparkline, children } = $props<{
    label: string
    value?: string | number
    tone?: 'default' | 'soft' | 'disabled' | 'accent' | 'danger' | 'success' | 'warning'
    /** relative change, e.g. 12.5 for +12.5 % */
    delta?: number
    invert?: boolean
    hint?: string
    sparkline?: Snippet
    children?: Snippet
  }>()

  const up = $derived((delta ?? 0) > 0)
  const deltaTone = $derived(!delta ? 'soft' : (invert ? !up : up) ? 'success' : 'danger')
</script>

<div class="stat">
  <Text variant="caption" text={label} />
  {#if children}
    {@render children()}
  {:else}
    <Text variant="value" {tone} text={String(value ?? '')} />
  {/if}
  {#if delta !== undefined}
    <span class="delta">
      <Icon name={delta === 0 ? 'remove' : up ? 'trending_up' : 'trending_down'} size="sm" tone={deltaTone} />
      <Text size="sm" tone={deltaTone} text={`${delta > 0 ? '+' : ''}${delta}%`} />
    </span>
  {/if}
  {#if sparkline}<span class="spark">{@render sparkline()}</span>{/if}
  {#if hint}<Text variant="caption" text={hint} />{/if}
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .stat {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--fui-space-1);
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .delta { display: inline-flex; align-items: center; gap: var(--fui-space-1); }
  .spark { display: block; }
</style>
