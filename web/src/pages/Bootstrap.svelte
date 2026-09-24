<script lang="ts">
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import { squircle } from '../lib/squircle'

  let { ondone } = $props<{ ondone: () => void }>()

  let username = $state('')
  let password = $state('')
  let password2 = $state('')
  let error = $state('')
  let loading = $state(false)

  async function submit(): Promise<void> {
    error = ''
    if (password !== password2) { error = 'Passwords do not match.'; return }
    loading = true
    try {
      await api.bootstrap(username, password)
      ondone?.()
    } catch (e) {
      error = getErrorMessage(e) || 'Failed to create account.'
    } finally {
      loading = false
    }
  }
</script>

<div class="auth-wrap">
  <div class="auth-card" use:squircle={18}>
    <div class="brand">llm-router</div>
    <h1>Create admin account</h1>
    <p class="sub">First run — set up your dashboard credentials.</p>

    {#if error}
      <div class="error-msg" use:squircle={12}>{error}</div>
    {/if}

    <div class="form-group">
      <label for="u">Username</label>
      <input id="u" type="text" bind:value={username} autocomplete="username" use:squircle={12} />
    </div>
    <div class="form-group" style="margin-top: 12px;">
      <label for="p">Password</label>
      <input id="p" type="password" bind:value={password} autocomplete="new-password" use:squircle={12} />
      {#if password.length > 0 && password.length < 8}
        <div class="field-hint">Recommendation: use at least 8 characters for a stronger password.</div>
      {/if}
    </div>
    <div class="form-group" style="margin-top: 12px;">
      <label for="p2">Confirm password</label>
      <input id="p2" type="password" bind:value={password2} autocomplete="new-password" onkeydown={(e) => e.key === 'Enter' && submit()} use:squircle={12} />
    </div>
    <button class="btn btn-primary submit-btn" onclick={submit} disabled={loading} use:squircle={12}>
      {loading ? 'Creating…' : 'Create account'}
    </button>
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
    background: var(--color-surface);
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
  .field-hint {
    margin-top: 6px;
    font-size: var(--text-sm);
    color: var(--color-warning-text);
  }
  /* Same shape and height as the text fields above: field line-height and
     paddings, global control radius. */
  .submit-btn {
    width: 100%;
    justify-content: center;
    margin-top: 20px;
    line-height: 20px;
    padding: var(--field-pad-v) var(--field-pad-h);
    transition: transform 120ms ease;
  }
  .submit-btn:active {
    transform: scale(0.97);
  }
</style>
