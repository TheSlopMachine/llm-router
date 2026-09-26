<script lang="ts">
  // The one layout primitive. VStack / HStack are three-line aliases over it.
  //
  // Structural rules live scoped below (.stk); everything variable arrives
  // as inline custom properties. .stk-grow stays global in app.css:
  // ScrollView consumes it outside Stack.
  import type { Snippet } from 'svelte'
  import { ALIGN, JUSTIFY, space, type Step, type Align, type Justify } from '../tokens'

  let {
    axis = 'v',
    gap = 0,
    pad,
    align,
    justify = 'start',
    wrap = false,
    grow = false,
    fill = false,
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
    fill?: boolean
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
  class:stk-grow={grow}
  class:stk-fill={fill}
  class:stk-pad={pad !== undefined}
  class:stk-scroll-y={scroll === 'y'}
  class:stk-scroll-x={scroll === 'x'}
  style:--stk-gap={space(gap)}
  style:--stk-pad={space(pad)}
  style:--stk-align={alignValue}
  style:--stk-justify={JUSTIFY[justify as Justify]}
  {...rest}
>
  {@render children()}
</svelte:element>

<style>
  .stk {
    display: flex;
    flex-direction: column;
    gap: var(--stk-gap, 0px);
    align-items: var(--stk-align, stretch);
    justify-content: var(--stk-justify, flex-start);
    min-width: 0;
    /* flex children refuse to shrink below content width without this; it is
       the single most common cause of overflowing rows, so it is the default */
    min-height: 0;
  }
  .stk-h {
    flex-direction: row;
    align-items: var(--stk-align, center);
  }
  .stk-wrap { flex-wrap: wrap; }
  .stk-fill { width: 100%; }
  .stk-scroll-y { overflow-y: auto; }
  .stk-scroll-x { overflow-x: auto; }
  .stk-pad { padding: var(--stk-pad, 0px); }
</style>
