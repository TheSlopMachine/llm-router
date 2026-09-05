<script lang="ts">
  let {
    value = $bindable(''),
    placeholder = 'Search...',
    disabled = false
  } = $props<{
    value?: string
    placeholder?: string
    disabled?: boolean
  }>()
</script>

<div class="search-field">
  <span class="icon search-icon">search</span>
  <input type="text" {placeholder} bind:value {disabled} />
</div>

<style>
  /* layout provided by global .search-field/.search-icon; keep scoped for project isolation if globals missing */
  .search-field {
    position: relative;
    margin-bottom: 16px;
  }

  .search-field input {
    padding-left: 36px;
  }

  .search-icon {
    position: absolute;
    left: 10px;
    top: 50%;
    transform: translateY(-50%);
    font-size: 18px;
    color: var(--color-text-soft);
    pointer-events: none;
  }
</style>
