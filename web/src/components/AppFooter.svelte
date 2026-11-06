<script lang="ts">
  import { Button, CopyButton, Footer, Spacer, Text } from '@fuckyouryan/fui'
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { t, tx } from '../lib/i18n.svelte'

  const REPOSITORY_URL = 'https://github.com/TheSlopMachine/llm-router'
  // The origin the dashboard was reached with also serves /v1 (vite proxies it in dev).
  const apiUrl = window.location.origin + '/v1'

  let version = $state('')

  onMount(() => {
    api.status()
      .then((status) => { version = status.version })
      .catch((error: unknown) => { console.error('Router version fails to load', error) })
  })

  // Release builds report "X.Y[.Z]"; dev builds report a bare word such as "dev".
  let versionLabel = $derived(/^\d/.test(version) ? 'v' + version : version)
</script>

<Footer>
  <Text size="sm" foregroundStyle="secondary" whiteSpace="nowrap" text={tx('nav.footer.app_version', { version: versionLabel }).trim()} />
  <Button href={REPOSITORY_URL} external buttonStyle="text" size="sm" text={t('nav.footer.source_code')} />
  <Spacer />
  <Text size="sm" foregroundStyle="secondary" truncate text={tx('nav.footer.api_url', { url: apiUrl })} />
  <CopyButton text={apiUrl} title={t('nav.footer.copy_api_url')} ariaLabel={t('nav.footer.copy_api_url')} />
</Footer>
