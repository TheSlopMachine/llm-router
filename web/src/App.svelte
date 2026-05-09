<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from './lib/api'
  import Login from './pages/Login.svelte'
  import Bootstrap from './pages/Bootstrap.svelte'
  import Dashboard from './pages/Dashboard.svelte'
  import Modal from './components/Modal.svelte'

  type AppState = 'loading' | 'bootstrap' | 'login' | 'dashboard'

  let state: AppState = 'loading'
  let error: string | null = null

  onMount(async (): Promise<void> => {
    const path = window.location.pathname
    
    try {
      const status = await api.status()
      
      if (!status.bootstrapped) {
        if (path !== '/bootstrap') {
          window.location.replace('/bootstrap')
          return
        }
        state = 'bootstrap'
      } else if (!status.authenticated) {
        if (path !== '/login') {
          window.location.replace('/login')
          return
        }
        state = 'login'
      } else {
        if (path !== '/') {
          window.location.replace('/#/overview')
          return
        }
        if (!window.location.hash) {
          window.location.hash = '#/overview'
        }
        state = 'dashboard'
      }
    } catch (e) {
      error = (e as Error).message
      if (path !== '/login') {
        window.location.replace('/login')
        return
      }
      state = 'login'
    }
  })

  function onLogin(): void {
    window.location.replace('/#/overview')
  }
  
  function onBootstrap(): void {
    window.location.replace('/login')
  }
  
  function onLogout(): void {
    window.location.replace('/login')
  }
</script>

{#if state === 'loading'}
  <div class="splash">
    {#if error}
      <div style="max-width: 400px; text-align: center;">
        <div style="font-size: 20px; font-weight: 600; margin-bottom: 16px;">llm-router</div>
        <div class="error-msg">{error}</div>
      </div>
    {:else}
      llm-router
    {/if}
  </div>
{:else if state === 'bootstrap'}
  <Bootstrap on:done={onBootstrap} />
{:else if state === 'login'}
  <Login on:done={onLogin} />
{:else}
  <Dashboard on:logout={onLogout} />
{/if}

<Modal />

<style>
  :global(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :global(body) {
    font-family: "Inter", system-ui, -apple-system, sans-serif;
    font-size: 14px;
    font-weight: 400;
    line-height: 21px;
    background: var(--color-background);
    color: var(--color-text);
    font-optical-sizing: auto;
    -webkit-font-smoothing: antialiased;
  }
  :global(a) { 
    color: #2483e2; 
    font-weight: 500;
    text-decoration: none; 
    cursor: pointer;
  }
  :global(a:hover) { text-decoration: underline; }

  :global(:root) {
    /* Light Theme Tokens */
    --color-surface: #ffffff;
    --color-surface-container: #ffffff;
    --color-surface-container-high: #fcfcfc;
    --color-surface-container-highest: #f4f5f5;
    --color-background: #fafafa;
    
    --color-sidebar-bg: #fafafa;
    --color-nav-hover: #efefef;
    --color-nav-active: #ececec;
    
    --color-text: #2b2d31;
    --color-text-soft: #6c717a;
    --color-text-disabled: #bdc1c6;
    --color-text-link: #2483e2;
    --color-text-on-button: #2b2d31;
    --color-text-on-button-reverse: #ffffff;
    
    --color-outline-light: #e2e3e4;
    --color-outline-soft: #eaeaeb;
    --color-outline: #76777b;
    --color-outline-variant: #c6c6ca;
    
    --color-button-container: #ffffff;
    --color-button-container-high: #eaeaeb;
    --color-button-container-accent: #dbeafe;
    
    --color-hover-bg: #f4f5f5;
    
    --color-accent-yellow: #fcbd00;
    --color-accent-yellow-light: #fff7e0;
    
    --color-notification-info-bg: #eff6ff;
    --color-notification-info-border: #bfdbfe;
    --color-notification-info-text: #1e40af;
    --color-notification-info-icon: #3b82f6;
    --color-notification-warning-bg: #fef3c7;
    --color-notification-warning-border: #fde68a;
    --color-notification-warning-text: #92400e;
    --color-notification-warning-icon: #f59e0b;
    
    --color-badge-blue-bg: rgba(59, 130, 246, 0.1);
    --color-badge-blue-text: #2563eb;
    --color-badge-green-bg: rgba(34, 197, 94, 0.1);
    --color-badge-green-text: #16a34a;
    --color-badge-yellow-bg: rgba(234, 179, 8, 0.1);
    --color-badge-yellow-text: #ca8a04;
    --color-badge-red-bg: rgba(239, 68, 68, 0.1);
    --color-badge-red-text: #dc2626;
    
    --color-warning-text: #ca8a04;
    --color-error-text: #dc2626;
    --color-success-text: #16a34a;
    
    --shadow-xs: 0 1px 2px 0 rgba(0, 0, 0, 0.05);
    --shadow-sm: 0 1px 3px 0 rgba(10, 13, 18, 0.1), 0 1px 2px -1px rgba(10, 13, 18, 0.1);
    --shadow-md: 0 4px 6px -1px rgba(10, 13, 18, 0.1), 0 2px 4px -2px rgba(10, 13, 18, 0.06);
    --shadow-lg: 0 12px 16px -4px rgba(10, 13, 18, 0.08), 0 4px 6px -2px rgba(10, 13, 18, 0.03);
    --shadow-xl: 0 20px 24px -4px rgba(10, 13, 18, 0.08), 0 8px 8px -4px rgba(10, 13, 18, 0.03);
    
    --sidebar-w: 240px;
  }

  :global(.dark) {
    /* Dark Theme Tokens */
    --color-surface: #1f1f1f;
    --color-surface-container: #1a1a1a;
    --color-surface-container-high: #252525;
    --color-surface-container-highest: #2a2a2a;
    --color-background: #141313;
    
    --color-sidebar-bg: #1a1a1a;
    --color-nav-hover: #252525;
    --color-nav-active: #2a2a2a;
    
    --color-text: #e5e2e1;
    --color-text-soft: #a0a0a0;
    --color-text-disabled: #4a4a4a;
    --color-text-link: #87a9ff;
    --color-text-on-button: #e5e2e1;
    --color-text-on-button-reverse: #141313;
    
    --color-outline-light: #333333;
    --color-outline-soft: #262626;
    --color-outline: #8e9192;
    --color-outline-variant: #444748;
    
    --color-button-container: #2a2a2a;
    --color-button-container-high: #353535;
    --color-button-container-accent: #2d3548;
    
    --color-hover-bg: #252525;
    
    --color-accent-yellow: #fcbd00;
    --color-accent-yellow-light: #3a321b;
    
    --color-notification-info-bg: #1e2a3a;
    --color-notification-info-border: #2d4a6f;
    --color-notification-info-text: #93c5fd;
    --color-notification-info-icon: #60a5fa;
    --color-notification-warning-bg: #3a321b;
    --color-notification-warning-border: #4a4020;
    --color-notification-warning-text: #fbbf24;
    --color-notification-warning-icon: #f59e0b;
    
    --color-badge-blue-bg: rgba(96, 165, 250, 0.15);
    --color-badge-blue-text: #60a5fa;
    --color-badge-green-bg: rgba(74, 222, 128, 0.15);
    --color-badge-green-text: #4ade80;
    --color-badge-yellow-bg: rgba(251, 191, 36, 0.15);
    --color-badge-yellow-text: #fbbf24;
    --color-badge-red-bg: rgba(248, 113, 113, 0.15);
    --color-badge-red-text: #f87171;
    
    --color-warning-text: #fbbf24;
    --color-error-text: #f87171;
    --color-success-text: #4ade80;
    
    --shadow-xs: 0 1px 2px 0 rgba(0, 0, 0, 0.5);
    --shadow-sm: 0 1px 3px 0 rgba(0, 0, 0, 0.6), 0 1px 2px -1px rgba(0, 0, 0, 0.6);
    --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.6), 0 2px 4px -2px rgba(0, 0, 0, 0.5);
    --shadow-lg: 0 12px 16px -4px rgba(0, 0, 0, 0.6), 0 4px 6px -2px rgba(0, 0, 0, 0.5);
    --shadow-xl: 0 20px 24px -4px rgba(0, 0, 0, 0.6), 0 8px 8px -4px rgba(0, 0, 0, 0.5);
  }

  :global(input, select, textarea) {
    font-family: inherit;
    font-size: 14px;
    font-weight: 400;
    width: 100%;
    border-radius: 8px;
    border: 1px solid var(--color-outline-light);
    background: var(--color-surface);
    color: var(--color-text);
    padding: 6px 12px;
    outline: none;
  }
  :global(input:focus, select:focus, textarea:focus) {
    border-color: var(--color-text-soft);
  }
  :global(input:disabled, select:disabled, textarea:disabled) {
    background-color: var(--color-surface-container);
    color: var(--color-text-disabled);
    border-color: var(--color-outline-soft);
    cursor: not-allowed;
  }
  :global(textarea) {
    resize: vertical;
    min-height: 80px;
  }
  :global(select option) { 
    background: var(--color-surface); 
    color: var(--color-text);
  }

  :global(button), :global(.btn) {
    font-family: inherit;
    font-size: 14px;
    font-weight: 500;
    line-height: 21px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    padding: 0 16px;
    height: 32px;
    border-radius: 12px;
    cursor: pointer;
    border: 1px solid transparent;
    background: transparent;
    color: var(--color-text-on-button);
    white-space: nowrap;
    user-select: none;
  }
  :global(button:hover:not([disabled])), :global(.btn:hover:not([disabled])) { 
    background: var(--color-button-container-high); 
  }
  :global(button:disabled), :global(.btn:disabled) { 
    background-color: var(--color-surface-container);
    color: var(--color-text-disabled);
    cursor: not-allowed;
    opacity: 0.6;
  }
  :global(button:focus-visible), :global(.btn:focus-visible) {
    outline: 2px solid var(--color-outline);
    outline-offset: -2px;
  }

  :global(.btn-primary) {
    background: var(--color-button-container);
    border-color: var(--color-outline-light);
    color: var(--color-text-on-button);
  }
  :global(.btn-primary:hover:not([disabled])) { 
    background: var(--color-button-container-high);
  }
  
  :global(.btn-secondary) {
    border-color: transparent;
    background: transparent;
    color: var(--color-text-on-button);
  }
  :global(.btn-secondary:hover:not([disabled])) {
    background: var(--color-button-container-high);
  }
  
  :global(.btn-danger) {
    background: transparent;
    border-color: var(--color-error-text);
    color: var(--color-error-text);
  }
  :global(.btn-danger:hover:not([disabled])) { 
    background: rgba(239, 68, 68, 0.1); 
  }
  
  :global(.btn-link) {
    color: var(--color-text-link);
    border: none;
    background: transparent;
  }
  :global(.btn-link:hover) { 
    text-decoration: underline;
    background: transparent;
  }
  
  :global(.btn-sm) { 
    padding: 0 12px; 
    height: 28px;
    font-size: 12px; 
  }
  
  :global(.btn-large) {
    height: 36px;
  }
  
  :global(.btn-icon) {
    aspect-ratio: 1 / 1;
    padding: 0;
    border-radius: 50%;
  }

  :global(table) { width: 100%; border-collapse: collapse; }
  :global(th) {
    text-align: left;
    font-size: 12px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-soft);
    padding: 10px 16px;
    border-bottom: 1px solid var(--color-outline-light);
  }
  :global(td) {
    padding: 12px 16px;
    border-bottom: 1px solid var(--color-outline-soft);
    color: var(--color-text-soft);
    font-size: 14px;
  }
  :global(tr:last-child td) { border-bottom: none; }
  :global(tbody tr:hover td) { 
    background: var(--color-hover-bg);
  }

  :global(.badge) {
    display: inline-flex;
    align-items: center;
    padding: 3px 10px;
    border-radius: 9999px;
    font-size: 11px;
    font-weight: 500;
  }
  :global(.badge-blue) { background: rgba(59, 130, 246, 0.1); color: #2563eb; }
  :global(.badge-green) { background: rgba(34, 197, 94, 0.1); color: #16a34a; }
  :global(.badge-yellow) { background: rgba(234, 179, 8, 0.1); color: #ca8a04; }
  :global(.badge-red) { background: rgba(239, 68, 68, 0.1); color: #dc2626; }

  :global(.mono) { 
    font-family: "DM Mono", "SF Mono", "Fira Code", monospace; 
    font-size: 13px; 
  }
  :global(.empty) { 
    padding: 48px; 
    text-align: center; 
    color: var(--color-text-soft);
    font-size: 14px;
  }

  :global(.error-msg) {
    background: rgba(239, 68, 68, 0.1);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: #dc2626;
    padding: 12px 16px;
    border-radius: 8px;
    margin-bottom: 16px;
    font-size: 13px;
  }
  :global(.success-msg) {
    background: rgba(34, 197, 94, 0.1);
    border: 1px solid rgba(34, 197, 94, 0.3);
    color: #16a34a;
    padding: 12px 16px;
    border-radius: 8px;
    margin-bottom: 16px;
    font-size: 13px;
  }

  :global(.card) {
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 16px;
    overflow: hidden;
  }
  :global(.card-header) {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 16px;
    border-bottom: 1px solid var(--color-outline-soft);
  }
  :global(.card-header h2) { 
    font-size: 16px; 
    font-weight: 500; 
    color: var(--color-text); 
  }

  :global(.form-group) { 
    display: flex; 
    flex-direction: column; 
    gap: 6px; 
  }
  :global(.form-group label) { 
    font-size: 12px; 
    font-weight: 500; 
    color: var(--color-text-soft); 
  }

  :global(.page-header) { margin-bottom: 24px; }
  :global(.page-header h1) { 
    font-size: 24px; 
    font-weight: 400; 
    margin-bottom: 4px;
    color: var(--color-text);
  }
  :global(.page-header p) { 
    color: var(--color-text-soft); 
    font-size: 14px; 
  }
  
  :global(.icon) {
    font-family: "Material Symbols Outlined", sans-serif;
    font-size: 20px;
    vertical-align: middle;
    display: inline-block;
    font-weight: normal;
    font-style: normal;
    line-height: 1;
    letter-spacing: normal;
    text-transform: none;
    white-space: nowrap;
    word-wrap: normal;
    direction: ltr;
    -webkit-font-feature-settings: "liga";
    font-variation-settings: "FILL" 0, "wght" 300, "GRAD" 0, "opsz" 20;
  }
  :global(.icon-filled) {
    font-variation-settings: "FILL" 1, "wght" 300, "GRAD" 0, "opsz" 20;
  }
  
  :global(.divider) {
    border: none;
    border-top: 1px solid var(--color-outline-soft);
    margin: 16px 0;
  }

  .splash {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100vh;
    font-size: 20px;
    font-weight: 600;
    color: var(--color-text-soft);
    letter-spacing: 0.1em;
  }
</style>