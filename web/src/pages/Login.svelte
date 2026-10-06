<script lang="ts">
  import { api } from '../lib/api'
  import { t } from '../lib/i18n.svelte'
  import { Button, TextEdit, Banner, VStack, Checkbox } from '$ui'
  import AuthCard from '../components/AuthCard.svelte'

  let { ondone } = $props<{ ondone: () => void }>()

  let username = $state('')
  let password = $state('')
  let rememberMe = $state(false)
  let error = $state('')
  let loading = $state(false)
  let isLoginReady = $derived(!!username && !!password && !loading)

  async function submit(): Promise<void> {
    if (!isLoginReady) return
    error = ''
    loading = true
    try {
      await api.login(username, password)
      ondone?.()
    } catch (e) {
      error = t('auth.errors.invalid_credentials')
    } finally {
      loading = false
    }
  }
</script>

<AuthCard title={t('auth.sign_in.title')} subtitle={t('auth.subtitle.manage')}>
  <VStack gap={4}>
    {#if error}
      <Banner variant="error" text={error} />
    {/if}

    <VStack gap={1}>
      <label for="u">{t('auth.username.label')}</label>
      <TextEdit id="u" bind:value={username} autocomplete="username" onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && submit()} />
    </VStack>
    <VStack gap={1}>
      <label for="p">{t('auth.password.label')}</label>
      <TextEdit id="p" type="secret" bind:value={password} autocomplete="current-password" onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && submit()} />
    </VStack>
    <Checkbox bind:checked={rememberMe} label={t('auth.remember_me.label')} />
    <Button style="prominent" block onclick={submit} disabled={!isLoginReady}>{loading ? t('auth.sign_in.signing_in') : t('auth.sign_in.title')}</Button>
  </VStack>
</AuthCard>
