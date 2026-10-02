<script lang="ts">
  import { api } from '../lib/api'
  import { t } from '../lib/i18n.svelte'
  import { squircle } from '../lib/squircle'
  import Button from '../components/ui/controls/Button.svelte'
  import TextEdit from '../components/ui/controls/TextEdit.svelte'
  import Checkbox from '../components/ui/controls/Checkbox.svelte'

  let { ondone } = $props<{ ondone: () => void }>()

  let username = $state('')
  let password = $state('')
  let rememberMe = $state(false)
  let error = $state('')
  let loading = $state(false)

  async function submit(): Promise<void> {
    if (!username || !password) return
    error = ''
    loading = true
    try {
      await api.login(username, password)
      ondone?.()
    } catch (e) {
      error = t('Invalid username or password.')
    } finally {
      loading = false
    }
  }
</script>

<div class="auth-wrap">
  <div class="auth-card" use:squircle={18}>
    <div class="brand">llm-router</div>
    <h1>{t('Sign in')}</h1>
    <p class="sub">{t('Manage providers, tokens, and credentials.')}</p>

    {#if error}
      <div class="error-msg" use:squircle={12}>{error}</div>
    {/if}

    <div class="form-group">
      <label for="u">{t('Username')}</label>
      <TextEdit id="u" bind:value={username} autocomplete="username" onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && submit()} />
    </div>
    <div class="form-group" style="margin-top: 12px;">
      <label for="p">{t('Password')}</label>
      <TextEdit id="p" type="secret" bind:value={password} autocomplete="current-password" onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && submit()} />
    </div>
    <div class="remember-me">
      <Checkbox bind:checked={rememberMe} label={t('Keep me signed in')} />
    </div>
    <div class="submit">
      <Button style="prominent" block onclick={submit} disabled={loading}>{loading ? t('Signing in…') : t('Sign in')}</Button>
    </div>
  </div>
</div>

<style>
  .auth-wrap {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-6);
    background: var(--color-background);
  }
  .auth-card {
    background: var(--color-surface-container-high);
    border: none;
    border-radius: var(--radius-lg);
    padding: 40px;
    width: 100%;
    max-width: 400px;
  }
  .brand {
    font-size: var(--text-sm);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--color-text);
    margin-bottom: var(--space-6);
  }
  h1 { 
    font-size: var(--text-xl); 
    font-weight: 600; 
    margin-bottom: 6px;
    color: var(--color-text);
  }
  .sub { 
    color: var(--color-text-soft); 
    font-size: var(--text-base); 
    margin-bottom: var(--space-6); 
  }
  .submit {
    margin-top: 20px;
  }
  .remember-me {
    margin-top: var(--space-5);
  }
</style>
