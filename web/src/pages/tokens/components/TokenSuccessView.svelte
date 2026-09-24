<script lang="ts">
  import { CodeBlock, Grid, VStack, Text } from '$ui'
  import { t } from '$lib/i18n.svelte'

  let {
    token,
    tokenName,
    scopeLabel,
    error = ''
  } = $props<{
    token: string | null
    tokenName: string
    scopeLabel: { providers: string; models: string; accounts: string }
    error?: string
  }>()
</script>

<VStack gap={4}>
  <VStack gap={1} align="center">
    <Text tag="h2" size="md" weight="medium" align="center">{t('Token {name} created').replace('{name}', tokenName)}</Text>
    <Text tag="h3" size="sm" tone="soft" align="center">{t('Copy it now — it will not be shown again.')}</Text>
  </VStack>

  {#if error}
    <Text tone="danger" size="sm">{error}</Text>
  {/if}

  <CodeBlock text={token ?? ''} />

  <Grid cols={3} gap={4} class="scope-summary">
    <VStack align="center" gap={2} class="scope-block">
      <Text size="xs" weight="medium" tone="soft" class="scope-label">{t('Providers')}</Text>
      <Text size="sm" weight="medium">{scopeLabel.providers}</Text>
    </VStack>
    <VStack align="center" gap={2} class="scope-block">
      <Text size="xs" weight="medium" tone="soft" class="scope-label">{t('Models')}</Text>
      <Text size="sm" weight="medium">{scopeLabel.models}</Text>
    </VStack>
    <VStack align="center" gap={2} class="scope-block">
      <Text size="xs" weight="medium" tone="soft" class="scope-label">{t('Accounts')}</Text>
      <Text size="sm" weight="medium">{scopeLabel.accounts}</Text>
    </VStack>
  </Grid>
</VStack>

<style>
  /* :global — every class below rides a ui-component root in another component. */
  :global(.scope-summary) {
    width: 100%;
  }

  :global(.scope-block) {
    padding: 14px 12px;
    border-radius: 12px;
    background: var(--elev);
    text-align: center;
  }

  :global(.scope-label) {
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
</style>
