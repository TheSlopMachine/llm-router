<script lang="ts">
  let {
    token,
    scopeLabel,
    copied = false,
    error = '',
    onCopy,
    onCreateAnother,
    onDone
  } = $props<{
    token: string | null
    scopeLabel: { providers: string; models: string; accounts: string }
    copied?: boolean
    error?: string
    onCopy: () => void
    onCreateAnother: () => void
    onDone: () => void
  }>()
</script>

<div class="success-view">
  <div class="success-icon-wrap">
    <span class="icon success-icon">check_circle</span>
  </div>
  <h3 class="success-title">Token created</h3>
  <p class="success-hint">Copy it now — it will not be shown again.</p>

  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  <div class="token-box">
    <span class="mono token-string">{token}</span>
    <button class="btn btn-secondary btn-sm" onclick={onCopy} aria-label="Copy token">
      <span class="icon" style="font-size: 16px;">{copied ? 'check' : 'content_copy'}</span>
      {copied ? 'Copied!' : 'Copy'}
    </button>
  </div>

  <div class="scope-summary">
    <div class="scope-block">
      <span class="scope-label">Providers</span>
      <span class="scope-value">{scopeLabel.providers}</span>
    </div>
    <div class="scope-block">
      <span class="scope-label">Models</span>
      <span class="scope-value">{scopeLabel.models}</span>
    </div>
    <div class="scope-block">
      <span class="scope-label">Accounts</span>
      <span class="scope-value">{scopeLabel.accounts}</span>
    </div>
  </div>
</div>

<style>
  .success-view {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
    padding: 16px 0;
    text-align: center;
  }

  .success-icon-wrap {
    width: 48px;
    height: 48px;
    border-radius: 50%;
    background: rgba(34, 197, 94, 0.1);
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .success-icon {
    font-size: 28px;
    color: var(--color-success-text);
  }

  .success-title {
    font-size: 18px;
    font-weight: 500;
    color: var(--color-text);
    margin: 0;
  }

  .success-hint {
    font-size: 13px;
    color: var(--color-text-soft);
    margin: 0;
  }

  .token-box {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    padding: 12px 14px;
    border-radius: 8px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface-container-highest);
    text-align: left;
  }

  .token-string {
    flex: 1;
    min-width: 0;
    word-break: break-all;
    font-size: 12px;
    color: var(--color-text);
  }

  .scope-summary {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 12px;
    width: 100%;
  }

  .scope-block {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 14px 12px;
    border-radius: 12px;
    border: 1px solid var(--color-outline-soft);
    background: var(--color-surface);
    text-align: center;
  }

  .scope-label {
    font-size: 11px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-soft);
  }

  .scope-value {
    font-size: 13px;
    font-weight: 500;
    color: var(--color-text);
  }
</style>
