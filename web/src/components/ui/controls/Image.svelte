<script lang="ts">
  // Worth a component only because it adds what a bare <img> lacks: an
  // aspect lock that reserves space before load, a fallback glyph on error,
  // and lazy loading by default.
  import type { Step } from '../tokens'

  let {
    src,
    alt = '',
    width,
    height,
    ratio,
    fit = 'contain',
    radius,
    fallbackIcon = 'broken_image',
    lazy = true,
    class: cls = '',
  } = $props<{
    src: string
    alt?: string
    width?: number | string
    height?: number | string
    /** e.g. "16 / 9" — reserves space so the layout does not jump on load */
    ratio?: string
    fit?: 'contain' | 'cover' | 'fill' | 'none' | 'scale-down'
    radius?: 'xs' | 'sm' | 'md' | 'lg' | Step
    fallbackIcon?: string
    lazy?: boolean
    class?: string
  }>()

  let failed = $state(false)

  const dim = (v: number | string | undefined): string | undefined =>
    v === undefined ? undefined : typeof v === 'number' ? `${v}px` : v

  const radValue = $derived(
    radius === undefined
      ? undefined
      : typeof radius === 'string'
        ? `var(--radius-${radius})`
        : `var(--radius-${radius <= 1 ? 'xs' : radius === 2 ? 'sm' : radius === 3 ? 'md' : 'lg'})`
  )
</script>

{#if failed || !src}
  <span
    class="icon img-fallback {cls}"
    style:width={dim(width)}
    style:height={dim(height)}
    style:aspect-ratio={ratio}
    aria-label={alt || undefined}
    role={alt ? 'img' : 'presentation'}
  >{fallbackIcon}</span>
{:else}
  <img
    class="img {cls}"
    {src}
    {alt}
    loading={lazy ? 'lazy' : 'eager'}
    decoding="async"
    style:width={dim(width)}
    style:height={dim(height)}
    style:aspect-ratio={ratio}
    style:--img-fit={fit}
    style:--img-radius={radValue}
    onerror={() => { failed = true }}
  />
{/if}

<style>
  .img-fallback {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--color-text-disabled);
  }
</style>
