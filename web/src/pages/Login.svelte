<script lang="ts">
  import { api } from '../lib/api'
  import { t } from '../lib/i18n.svelte'
  import { squircle } from '../lib/squircle'

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
  <div class="auth-card-shadow">
  <div class="auth-card" use:squircle={18}>
    <div class="brand">llm-router</div>
    <h1>{t('Sign in')}</h1>
    <p class="sub">{t('Manage providers, tokens, and credentials.')}</p>

    {#if error}
      <div class="error-msg">{error}</div>
    {/if}

    <div class="form-group">
      <label for="u">{t('Username')}</label>
      <input id="u" type="text" bind:value={username} autocomplete="username" onkeydown={(e) => e.key === 'Enter' && submit()} use:squircle={8} />
    </div>
    <div class="form-group" style="margin-top: 12px;">
      <label for="p">{t('Password')}</label>
      <input id="p" type="password" bind:value={password} autocomplete="current-password" onkeydown={(e) => e.key === 'Enter' && submit()} use:squircle={8} />
    </div>
    <label class="remember-me">
      <input type="checkbox" class="check" bind:checked={rememberMe} use:squircle={6} />
      <span>{t('Keep me signed in')}</span>
    </label>
    <button class="btn btn-primary submit-btn" onclick={submit} disabled={loading} use:squircle={12}>
      {loading ? t('Signing in…') : t('Sign in')}
    </button>
  </div>
  </div>
</div>

<style>
  .auth-wrap {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    background: var(--color-background);
  }
  /* drop-shadow follows the squircle clip-path, unlike box-shadow */
  .auth-card-shadow {
    filter: drop-shadow(0 4px 6px rgba(10, 13, 18, 0.12));
  }
  :global(.dark) .auth-card-shadow {
    filter: drop-shadow(0 4px 6px rgba(0, 0, 0, 0.6));
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
    transition: transform 120ms ease;
  }
  .submit-btn:active {
    transform: scale(0.97);
  }
  .auth-card input[type="text"],
  .auth-card input[type="password"] {
    background: var(--color-button-container-high);
    border-radius: 8px;
    outline: none;
    /* transparent idle ring so the focus ring can transition in */
    box-shadow: inset 0 0 0 2px transparent;
    transition: background-color 160ms ease, box-shadow 160ms ease, padding 160ms ease, transform 120ms ease;
  }
  .auth-card input[type="text"]:focus,
  .auth-card input[type="password"]:focus {
    background: var(--color-button-container-high);
    outline: none;
    /* +2px vertical padding: the field grows by the ring size, width fixed */
    padding: calc(var(--field-pad-v) + 1px) var(--field-pad-h);
    box-shadow: inset 0 0 0 2px #fff;
  }
  /* press only (mouse/touch): keyboard focus gets no scale */
  .auth-card input[type="text"]:active,
  .auth-card input[type="password"]:active {
    transform: scale(0.99);
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
</style>
