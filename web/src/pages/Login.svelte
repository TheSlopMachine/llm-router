<script lang="ts">
  import { api } from '../lib/api'
  import { t } from '../lib/i18n.svelte'

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
  <div class="auth-card">
    <div class="brand">llm-router</div>
    <h1>{t('Sign in')}</h1>
    <p class="sub">{t('Manage providers, tokens, and credentials.')}</p>

    {#if error}
      <div class="error-msg">{error}</div>
    {/if}

    <div class="form-group">
      <label for="u">{t('Username')}</label>
      <input id="u" type="text" bind:value={username} autocomplete="username" onkeydown={(e) => e.key === 'Enter' && submit()} />
    </div>
    <div class="form-group" style="margin-top: 12px;">
      <label for="p">{t('Password')}</label>
      <input id="p" type="password" bind:value={password} autocomplete="current-password" onkeydown={(e) => e.key === 'Enter' && submit()} />
    </div>
    <label class="remember-me">
      <input type="checkbox" bind:checked={rememberMe} />
      <span>{t('Keep me signed in')}</span>
    </label>
    <button class="btn btn-primary submit-btn" onclick={submit} disabled={loading}>
      {loading ? t('Signing in…') : t('Sign in')}
    </button>
  </div>
</div>

<style>
  .auth-wrap {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-background);
  }
  .auth-card {
    background: var(--color-surface-container-high);
    border: none;
    border-radius: var(--radius-lg);
    padding: 40px;
    width: 100%;
    max-width: 400px;
    box-shadow: var(--shadow-md);
  }
  .brand {
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--color-text);
    margin-bottom: 24px;
  }
  h1 { 
    font-size: 24px; 
    font-weight: 600; 
    margin-bottom: 6px;
    color: var(--color-text);
  }
  .sub { 
    color: var(--color-text-soft); 
    font-size: 14px; 
    margin-bottom: 24px; 
  }
  .submit-btn {
    width: 100%;
    justify-content: center;
    margin-top: 20px;
    height: 40px;
  }
  .remember-me {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 16px;
    font-size: 14px;
    color: var(--color-text);
    cursor: pointer;
    user-select: none;
  }
  .remember-me input[type="checkbox"] {
    width: auto;
    cursor: pointer;
  }
</style>
