<script lang="ts">
  import { onMount } from 'svelte'
  import { theme } from '$lib/theme.svelte'
  import type { Theme } from '$lib/theme.svelte'
  import { language } from '$lib/language.svelte'
  import type { Language } from '$lib/language.svelte'
  import { api } from '$lib/api'
  import { t } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { accent, accents } from '$lib/accent.svelte'
  import { Button, HStack, SectionCard, Select, Spacer, Switch, Text, VStack } from '$ui'
  import { squircle } from '$lib/squircle'

  let languageOptions = $derived([
    { value: 'auto', label: t('Auto') },
    { value: 'en', label: t('English') },
    { value: 'ru', label: t('Russian') }
  ])
  let themeOptions: Array<{ value: string; label: string }> = $derived([
    { value: 'auto', label: t('Auto') },
    { value: 'light', label: t('Light') },
    { value: 'dark', label: t('Dark') }
  ])
  let lang = $state<Language>(language.value)
  let th = $state<Theme>(theme.value)
  let isClusterNode = $state(false)
  let disableTelemetry = $state(false)
  // Pool settings pass through untouched: no UI controls edit them yet.
  let minDownloadSpeedKbps = $state(15000)
  let maxProxiesPerLocation = $state(10)
  let updateIntervalMinutes = $state(15)
  let saving = $state(false)
  let error = $state('')

  onMount(async () => {
    try {
      const cfg = await api.config.get()
      isClusterNode = cfg.is_cluster_node
      disableTelemetry = cfg.disable_telemetry
      minDownloadSpeedKbps = cfg.min_download_speed_kbps
      maxProxiesPerLocation = cfg.max_proxies_per_location
      updateIntervalMinutes = cfg.update_interval_minutes
    } catch (e) {
      error = getErrorMessage(e)
    }
  })

  async function save(): Promise<void> {
    if (saving) return
    saving = true
    error = ''
    try {
      language.value = lang
      theme.value = th
      await api.config.update({
        is_cluster_node: isClusterNode,
        disable_telemetry: disableTelemetry,
        min_download_speed_kbps: minDownloadSpeedKbps,
        max_proxies_per_location: maxProxiesPerLocation,
        update_interval_minutes: updateIntervalMinutes
      })
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      saving = false
    }
  }
</script>

<VStack gap={4}>
  <VStack gap={1}>
    <Text tag="h1" size="lg" weight="bold">{t('Settings')}</Text>
    <Text tone="soft" size="sm">{t('Preferences and instance configuration.')}</Text>
  </VStack>

  {#if error}
    <Text tone="danger" size="sm">{error}</Text>
  {/if}

  <SectionCard title="Preferences" description="This browser only">
    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="settings-language">{t('Language')}</Text>
      <Select value={lang} options={languageOptions} onchange={(v) => (lang = v as Language)} />
    </VStack>

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="settings-theme">{t('Theme')}</Text>
      <Select value={th} options={themeOptions} onchange={(v) => (th = v as Theme)} />
    </VStack>

    <VStack gap={1}>
      <Text size="sm" weight="medium">{t('Accent')}</Text>
      <div class="accent-grid">
        {#each accents as a}
          <button
            type="button"
            class="accent-swatch"
            class:selected={accent.value.name === a.name}
            style="background: {a.bg}; color: {a.text}"
            onclick={() => { accent.value = a }}
            title="{a.name} ({a.bg})"
            aria-label={a.name}
            use:squircle={12}
          >Aa</button>
        {/each}
      </div>
    </VStack>
  </SectionCard>

  <SectionCard title="Instance configuration" description="This deployment">
    <HStack align="center" gap={4}>
      <Text>{t('Make this instance a cluster node')}</Text>
      <Spacer />
      <Switch bind:checked={isClusterNode} />
    </HStack>

    <HStack align="center" gap={4}>
      <Text>{t('Disable anonymized telemetry')}</Text>
      <Spacer />
      <Switch bind:checked={disableTelemetry} />
    </HStack>
  </SectionCard>

  <HStack justify="end">
    <Button style="prominent" onclick={save} disabled={saving}>
      {saving ? t('Saving...') : t('Save changes')}
    </Button>
  </HStack>
</VStack>

<style>
  .accent-grid {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
  .accent-swatch {
    width: 44px;
    height: 44px;
    border-radius: var(--ctl-radius);
    border: none;
    font-size: var(--text-base);
    font-weight: 700;
    cursor: pointer;
    padding: 0;
  }
  .accent-swatch.selected {
    outline: 2px solid var(--color-text);
    outline-offset: 2px;
  }
</style>
