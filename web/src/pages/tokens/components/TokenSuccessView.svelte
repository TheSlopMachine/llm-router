<script lang="ts">
  import { CodeBlock, Grid, VStack, Text, Banner, Box } from '$ui'
  import { t } from '$lib/i18n.svelte'

  let {
    token,
    tokenName,
    scopeLabel,
    error = ''
  } = $props<{
    token: string | null
    tokenName: string
    scopeLabel: { providers: string; models: string; credentials: string }
    error?: string
  }>()
</script>

<VStack gap={4}>
  <VStack gap={1} align="center">
    <Text tag="h2" size="md" weight="medium" align="center">{t('tokens.success.created_named').replace('{name}', tokenName)}</Text>
    <Text tag="h3" size="sm" tone="soft" align="center">{t('tokens.success.copy_now')}</Text>
  </VStack>

  {#if error}
    <Banner variant="error" text={error} />
  {/if}

  <CodeBlock text={token ?? ''} />

  <Grid cols={3} gap={4} class="scope-summary">
    <Box elev radius="md" pad={4}>
      <VStack align="center" gap={2}>
        <Text size="xs" weight="medium" tone="soft" class="scope-label">{t('providers.list.title')}</Text>
        <Text size="sm" weight="medium">{scopeLabel.providers}</Text>
      </VStack>
    </Box>
    <Box elev radius="md" pad={4}>
      <VStack align="center" gap={2}>
        <Text size="xs" weight="medium" tone="soft" class="scope-label">{t('models.list.title')}</Text>
        <Text size="sm" weight="medium">{scopeLabel.models}</Text>
      </VStack>
    </Box>
    <Box elev radius="md" pad={4}>
      <VStack align="center" gap={2}>
        <Text size="xs" weight="medium" tone="soft" class="scope-label">{t('credentials.title_plural')}</Text>
        <Text size="sm" weight="medium">{scopeLabel.credentials}</Text>
      </VStack>
    </Box>
  </Grid>
</VStack>

<style>
  /* :global — every class below rides a ui-component root in another component. */
  :global(.scope-summary) {
    width: 100%;
  }

  :global(.scope-label) {
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
</style>
