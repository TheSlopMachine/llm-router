<script lang="ts">
  // Eats the free space along the parent stack's axis. The flex equivalent of
  // SwiftUI's Spacer(), and the reason most `justify-content: space-between`
  // hacks are unnecessary. With `size` it becomes a fixed gap on `axis`
  // instead of growing (for backend-driven fixed spacers).
  import { space, type Step } from '../tokens'

  let {
    size,
    axis = 'y',
    grow = true,
    class: cls = '',
  } = $props<{
    /** fixed gap step; unset means grow along the parent axis */
    size?: Step
    axis?: 'x' | 'y'
    grow?: boolean
    class?: string
  }>()
</script>

<div
  class="spc {cls}"
  class:spc-fixed={size !== undefined}
  class:spc-static={size === undefined && !grow}
  style:width={size !== undefined && axis === 'x' ? space(size) : undefined}
  style:height={size !== undefined && axis === 'y' ? space(size) : undefined}
  aria-hidden="true"
></div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  /* Spacer: eats the free space along the parent axis. */
  .spc { flex: 1 1 auto; min-width: 0; min-height: 0; }
  .spc-fixed,
  .spc-static { flex: none; }
</style>
