<script lang="ts">
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import { t } from '../lib/i18n.svelte'
  import { Button, TextField, Banner, VStack, HStack, Checkbox, Text } from '@fuckyouryan/fui'

  let { ondone }: { ondone: () => void } = $props()

  // Server text of a 429 answer from POST /api/llm-router/login.
  const throttledError = 'too many failed logins'

  let password = $state('')
  let rememberMe = $state(false)
  let error = $state('')
  let loading = $state(false)
  let isLoginReady = $derived(!!password && !loading)

  async function submit(): Promise<void> {
    if (!isLoginReady) return
    error = ''
    loading = true
    try {
      await api.login(password, rememberMe)
      ondone?.()
    } catch (e) {
      error = getErrorMessage(e) === throttledError ? t('auth.errors.throttled') : t('auth.errors.invalid_password')
    } finally {
      loading = false
    }
  }
</script>

<div class="login-page">
  <VStack gap={7} class="login-form">
    <VStack gap={3} halign="center">
      <!-- App icon mark: outlined hexagon standing on a vertex. -->
      <svg class="mark" viewBox="0 0 100 100" aria-hidden="true">
        <polygon points="50,8 86.4,29 86.4,71 50,92 13.6,71 13.6,29" />
      </svg>
      <Text tag="h1" size={32} weight="medium">llm-router</Text>
    </VStack>
    <VStack gap={4}>
      {#if error}
        <Banner tint="danger" text={error} />
      {/if}
      <HStack gap={2}>
        <TextField
          id="password"
          secure
          fill
          size="lg"
          name="current-password"
          autocomplete="current-password"
          placeholder={t('auth.password.label')}
          ariaLabel={t('auth.password.label')}
          bind:value={password}
          onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && submit()}
        />
        <Button
          buttonStyle="prominent"
          size="lg"
          icon={{ name: 'arrow_forward' }}
          ariaLabel={t('auth.sign_in.title')}
          title={t('auth.sign_in.title')}
          loading={loading}
          disabled={!password}
          onclick={submit}
        />
      </HStack>
      <Checkbox bind:checked={rememberMe} label={t('auth.remember_me.label')} />
    </VStack>
  </VStack>
</div>

<style>
  .login-page {
    min-height: 100vh;
    min-height: 100dvh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--fui-space-6);
    /* Extra bottom padding lifts the block to the optical center. */
    padding-bottom: calc(var(--fui-space-6) + 10vh);
    background: var(--fui-color-background);
  }
  .mark {
    width: 56px;
    height: 56px;
    fill: none;
    stroke: var(--fui-color-text);
    stroke-width: 7;
    stroke-linejoin: round;
  }
  :global(.login-form) {
    width: 100%;
    max-width: var(--app-auth-max-w);
  }
</style>
