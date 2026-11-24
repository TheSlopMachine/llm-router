<script lang="ts">
  import { CodeBlock, Grid, Stat, VStack, Text, Banner } from '@fuckyouryan/fui'
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

  <Grid min="sm" gap={4}>
    {#each [
      { label: t('providers.list.title'), value: scopeLabel.providers },
      { label: t('models.list.title'), value: scopeLabel.models },
      { label: t('credentials.title_plural'), value: scopeLabel.credentials },
    ] as scope (scope.label)}
      <VStack elev radius="md" pad={4}>
        <Stat label={scope.label} value={scope.value} />
      </VStack>
    {/each}
  </Grid>
</VStack>
