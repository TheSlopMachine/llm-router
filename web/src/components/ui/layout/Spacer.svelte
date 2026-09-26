<script lang="ts">
  // Eats the free space along the parent stack's axis. The flex equivalent of
  // SwiftUI's Spacer(), and the reason most `justify-content: space-between`
  // hacks are unnecessary.
  let { class: cls = '' } = $props<{ class?: string }>()
</script>

<div class="spc {cls}" aria-hidden="true"></div>

<style>
  /* Spacer: eats the free space along the parent axis. */
  .spc { flex: 1 1 auto; min-width: 0; min-height: 0; }
</style>
