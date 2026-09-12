<script lang="ts">
  import ProviderAPIUsage from '../components/ProviderAPIUsage.svelte'
  import { t } from '../lib/i18n.svelte'
</script>

<div class="page-header">
  <div>
    <h1>{t('Metrics')}</h1>
    <p>{t('Provider API usage statistics and trends.')}</p>
  </div>
</div>

<ProviderAPIUsage />
