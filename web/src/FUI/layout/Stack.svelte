<script lang="ts">
  // The one layout primitive. VStack / HStack are three-line aliases over it.
  //
  // Structural rules live scoped below (.stk); everything variable arrives
  // as inline custom properties.
  import type { Snippet } from 'svelte'
  import { ALIGN, JUSTIFY, space, fillStyle, type Step, type Align, type Justify, type FillSize } from '../tokens'

  let {
    axis = 'v',
    gap = 0,
    pad,
    align,
    justify = 'start',
    wrap = false,
    grow = false,
    fill = undefined as FillSize | undefined,
    scroll,
    tag = 'div',
    class: cls = '',
    children,
    ...rest
  } = $props<{
    axis?: 'v' | 'h'
    /** spacing scale step, not pixels */
    gap?: Step
    pad?: Step
    align?: Align
    justify?: Justify
    wrap?: boolean
    /** take the free space along the parent's axis */
    grow?: boolean
    fill?: FillSize
    scroll?: 'x' | 'y'
    tag?: 'div' | 'section' | 'nav' | 'header' | 'footer' | 'aside' | 'ul' | 'li' | 'form'
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()

  // align defaults differ by axis: a column stretches its children to full
  // width, a row centres them on the cross axis. Matches SwiftUI's VStack /
  // HStack defaults and is what nearly every call site wanted anyway.
  const alignValue = $derived(ALIGN[(align ?? (axis === 'h' ? 'center' : 'stretch')) as Align])
</script>

<svelte:element
  this={tag}
  class="stk {cls}"
  class:stk-h={axis === 'h'}
  class:stk-wrap={wrap}
  class:grow
  class:stk-pad={pad !== undefined}
  class:stk-scroll-y={scroll === 'y'}
  class:stk-scroll-x={scroll === 'x'}
  style:--fui-stk-gap={space(gap)}
  style:--fui-stk-pad={space(pad)}
  style:--fui-stk-align={alignValue}
  style:--fui-stk-justify={JUSTIFY[justify as Justify]}
  style:flex={fill != null && fill !== false ? fillStyle(fill) : undefined}
  style:min-width={fill != null && fill !== false ? '0' : undefined}
  {...rest}
>
  {@render children()}
</svelte:element>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .stk {
    display: flex;
    flex-direction: column;
    gap: var(--fui-stk-gap, 0px);
    align-items: var(--fui-stk-align, stretch);
    justify-content: var(--fui-stk-justify, flex-start);
    min-width: 0;
    /* flex children refuse to shrink below content width without this; it is
       the single most common cause of overflowing rows, so it is the default */
    min-height: 0;
  }
  .stk-h {
    flex-direction: row;
    align-items: var(--fui-stk-align, center);
  }
  .stk-wrap { flex-wrap: wrap; }
  .grow { flex: 1 1 0; }
  .stk-fill { width: 100%; }
  .stk-scroll-y { overflow-y: auto; }
  .stk-scroll-x { overflow-x: auto; }
  .stk-pad { padding: var(--fui-stk-pad, 0px); }
</style>
