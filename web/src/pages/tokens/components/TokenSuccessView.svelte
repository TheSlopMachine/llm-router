<script lang="ts">
  import { CodeBlock, Grid, VStack, Text, Banner } from '@fuckyouryan/fui'
  import { t, tx } from '$lib/i18n.svelte'

  let {
    token,
    tokenName,
    scopeLabel,
    error = ''
  }: {
    token: string | null
    tokenName: string
    scopeLabel: { providers: string; models: string; credentials: string }
    error?: string
  } = $props()
</script>

<VStack gap={4}>
  <VStack gap={1} halign="center">
    <Text variant="section-title" align="center">{tx('tokens.success.created_named', { name: tokenName })}</Text>
    <Text tag="h3" size="sm" foregroundStyle="secondary" align="center">{t('tokens.success.copy_now')}</Text>
  </VStack>

  {#if error}
    <Banner tint="danger" text={error} />
  {/if}

  <CodeBlock text={token ?? ''} />

  <Grid min="sm" gap={4} class="scope-summary">
    <VStack elev radius="md" pad={4}>
      <VStack halign="center" gap={2}>
        <Text size="xs" weight="medium" foregroundStyle="secondary" class="scope-label">{t('providers.list.title')}</Text>
        <Text size="sm" weight="medium">{scopeLabel.providers}</Text>
      </VStack>
    </VStack>
    <VStack elev radius="md" pad={4}>
      <VStack halign="center" gap={2}>
        <Text size="xs" weight="medium" foregroundStyle="secondary" class="scope-label">{t('models.list.title')}</Text>
        <Text size="sm" weight="medium">{scopeLabel.models}</Text>
      </VStack>
    </VStack>
    <VStack elev radius="md" pad={4}>
      <VStack halign="center" gap={2}>
        <Text size="xs" weight="medium" foregroundStyle="secondary" class="scope-label">{t('credentials.title_plural')}</Text>
        <Text size="sm" weight="medium">{scopeLabel.credentials}</Text>
      </VStack>
    </VStack>
  </Grid>
</VStack>

<style>
  /* :global — every class below rides a ui-component root in another component. */
  :global(.scope-summary) {
    width: 100%;
  }

  :global(.scope-label) {
    text-transform: uppercase;
    letter-spacing: var(--fui-tracking-label);
  }
</style>
