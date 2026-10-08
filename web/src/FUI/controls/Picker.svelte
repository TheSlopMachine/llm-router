<script lang="ts">
  import { onMount } from 'svelte'
  import { squircle } from '../core/squircle'
  import type { Size } from '../tokens'

  let {
    value = $bindable(''),
    options,
    ariaLabel,
    size = 'medium',
    onchange
  } = $props<{
    value: string
    options: Array<{ value: string; label: string }>
    ariaLabel?: string
    size?: Size
    onchange?: (value: string) => void
  }>()

  function select(next: string): void {
    value = next
    onchange?.(next)
  }

  // Sliding selection pill: a single element that moves under the active
  // button instead of each button painting its own fill. Position is measured
  // from the DOM so it tracks label widths, metric vars and font loading.
  let rootEl = $state<HTMLElement>()
  let pillX = $state(0)
  let pillW = $state(0)
  // No transition before the first measure: the pill must appear in place,
  // not slide in from position zero.
  let pillReady = $state(false)

  function measure(): void {
    if (!rootEl) return
    const idx = options.findIndex((o: { value: string; label: string }) => o.value === value)
    const btn = rootEl.querySelectorAll<HTMLElement>('.seg-btn')[idx]
    if (!btn) return
    pillX = btn.offsetLeft
    pillW = btn.offsetWidth
  }

  onMount(() => {
    measure()
    pillReady = true
    const ro = new ResizeObserver(measure)
    ro.observe(rootEl!)
    return () => ro.disconnect()
  })

  $effect(() => {
    void value
    measure()
  })
</script>

<div class="segmented" data-size={size} role="group" aria-label={ariaLabel} bind:this={rootEl} use:squircle={12}>
  <span
    class="seg-pill"
    class:ready={pillReady}
    style="transform: translateX({pillX}px); width: {pillW}px"
    use:squircle={9}
  ></span>
  {#each options as opt, i}
    {#if i > 0}
      <!-- Divider is a sibling, not part of the pill: every segment stays
           symmetric, and the reserved slot keeps selection from shifting layout. -->
      <span class="seg-div" class:visible={value !== options[i - 1].value && value !== opt.value}></span>
    {/if}
    <button
      type="button"
      class="seg-btn"
      class:active={value === opt.value}
      aria-pressed={value === opt.value}
      onclick={() => select(opt.value)}
    >
      {opt.label}
    </button>
  {/each}
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  :where(button:disabled) { cursor: not-allowed; }
  /* Concentric corners: pill radius + container padding = container radius,
     both derived from the system control radius. Height comes from content
     (line-height + shared button padding), never fixed pixels. */
  .segmented {
    height: var(--fui-ctl-medium);
    position: relative;
    display: inline-flex;
    align-items: stretch;
    align-self: flex-start;
    padding: 3px;
    background: var(--fui-elev);
    border: none;
    border-radius: calc(var(--fui-ctl-radius) * 0.75 + 3px);
  }

  .seg-pill {
    position: absolute;
    top: 3px;
    bottom: 3px;
    left: 0;
    background: var(--fui-color-accent);
    border-radius: calc(var(--fui-ctl-radius) * 0.75);
    pointer-events: none;
  }
  .seg-pill.ready {
    /* smooth slide, no overshoot */
    transition:
      transform 0.25s cubic-bezier(0.4, 0, 0.2, 1),
      width 0.25s cubic-bezier(0.4, 0, 0.2, 1);
  }

  /* position:relative lifts labels above the absolutely-positioned pill. */
  .seg-btn {
    position: relative;
    display: flex;
    align-items: center;
    padding: var(--fui-btn-pad-v) var(--fui-btn-pad-h);
    border-radius: calc(var(--fui-ctl-radius) * 0.75);
    border: none;
    background: transparent;
    color: var(--fui-color-text-soft);
    font-size: var(--fui-text-base);
    line-height: 22px;
    font-weight: 500;
    cursor: pointer;
    transition: color 0.15s;
  }

  .segmented[data-size='small'] {
    height: var(--fui-ctl-small);
  }
  .segmented[data-size='large'] {
    height: var(--fui-ctl-large);
  }
  .segmented[data-size='small'] .seg-btn {
    line-height: 18px;
    padding: 2px var(--fui-btn-pad-h);
    font-size: var(--fui-text-sm);
  }
  .segmented[data-size='large'] .seg-btn {
    line-height: 24px;
    padding: 6px var(--fui-btn-pad-h);
  }

  /* No hover fill, no press bounce: selection (the sliding pill) is the only
     state this control shows. */
  .seg-btn:focus-visible {
    outline: none;
    box-shadow: var(--fui-focus-ring);
  }

  .seg-div {
    align-self: center;
    width: 1px;
    height: 16px;
    flex: none;
    background: transparent;
  }
  .seg-div.visible {
    background: var(--fui-color-outline-variant);
  }

  .seg-btn.active {
    color: var(--fui-color-text-on-button-reverse);
    font-weight: 600;
  }
</style>
