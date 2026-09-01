<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { api } from '../lib/api'
  import Dropdown from './Dropdown.svelte'
  import ActionDropdown from './ActionDropdown.svelte'
  import type { AvailableModel } from '../lib/types'

  type Role = 'user' | 'assistant'

  interface CodeArtifact {
    title: string
    code: string
    language?: string
    collapsed?: boolean
  }

  interface Message {
    id: string
    role: Role
    content: string
    timestamp: Date
    artifacts?: CodeArtifact[]
  }

  const STORAGE_HISTORY = 'llm-router:chat:history'
  const STORAGE_MODEL = 'llm-router:chat:model'

  let models: AvailableModel[] = []
  let selectedModel: string = ''
  let input = ''
  let isSending = false
  let error: string | null = null
  let copiedArtifact: string | null = null
  let messages: Message[] = []
  let threadEl: HTMLDivElement
  let textareaEl: HTMLTextAreaElement
  let fileInputEl: HTMLInputElement
  let hydrated = false

  const actions = [
    { id: 'new', label: 'New Chat', icon: 'add' },
    { id: 'save', label: 'Save', icon: 'download' },
    { id: 'load', label: 'Load', icon: 'upload' }
  ]

  onMount(async () => {
    try {
      const res = await api.models.available()
      models = Array.isArray(res) ? (res as AvailableModel[]) : []
    } catch {
      models = []
    }

    // restore model (independent of history)
    try {
      const savedModel = localStorage.getItem(STORAGE_MODEL)
      if (savedModel && models.some((m) => m.full_model_id === savedModel)) {
        selectedModel = savedModel
      } else if (models.length > 0 && !selectedModel) {
        selectedModel = models[0].full_model_id
      }
    } catch {}

    // restore single global history — independent of model
    try {
      const raw = localStorage.getItem(STORAGE_HISTORY)
      if (raw) {
        const parsed = JSON.parse(raw)
        if (Array.isArray(parsed)) {
          messages = parsed
            .filter((m: any) => m && (m.role === 'user' || m.role === 'assistant') && typeof m.content === 'string')
            .map((m: any) => ({
              ...m,
              timestamp: m.timestamp ? new Date(m.timestamp) : new Date(),
              artifacts: Array.isArray(m.artifacts) ? m.artifacts : undefined
            }))
        }
      }
    } catch {}

    // if still no model selected after history restore
    if (!selectedModel && models.length > 0) {
      selectedModel = models[0].full_model_id
    }

    hydrated = true
    await tick()
    scrollToBottom(false)
  })

  $: dropdownOptions = models.map((m) => ({ value: m.full_model_id, label: m.display_name || m.full_model_id }))
  $: canSend = input.trim().length > 0 && !isSending && !!selectedModel

  // persist global history — single last thread
  $: if (hydrated && typeof window !== 'undefined') {
    try {
      localStorage.setItem(STORAGE_HISTORY, JSON.stringify(messages))
    } catch {}
  }
  $: if (hydrated && typeof window !== 'undefined' && selectedModel) {
    try {
      localStorage.setItem(STORAGE_MODEL, selectedModel)
    } catch {}
  }

  function formatTime(d: Date): string {
    return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })
  }

  async function scrollToBottom(smooth = true) {
    await tick()
    if (threadEl) {
      threadEl.scrollTo({ top: threadEl.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
    }
  }

  function handleTextareaKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  function autoResize() {
    if (!textareaEl) return
    textareaEl.style.height = 'auto'
    textareaEl.style.height = Math.min(textareaEl.scrollHeight, 160) + 'px'
  }

  $: input, autoResize()

  function parseCodeArtifacts(content: string): { clean: string; artifacts: CodeArtifact[] } {
    const artifacts: CodeArtifact[] = []
    const re = /```(\w+)?\n([\s\S]*?)```/g
    let m: RegExpExecArray | null
    while ((m = re.exec(content)) !== null) {
      const lang = m[1]?.trim() || 'code'
      const code = m[2].trim()
      artifacts.push({ title: lang, code, language: lang, collapsed: false })
    }
    const clean = content.replace(re, '').trim()
    return { clean: clean || content, artifacts }
  }

  function handleAction(e: CustomEvent<string>) {
    const id = e.detail
    if (id === 'new') {
      messages = []
      error = null
      try { localStorage.removeItem(STORAGE_HISTORY) } catch {}
    } else if (id === 'save') {
      const blob = new Blob([JSON.stringify(messages, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'chat-history.json'
      a.click()
      URL.revokeObjectURL(url)
    } else if (id === 'load') {
      fileInputEl?.click()
    }
  }

  function handleFileChange(e: Event) {
    const inputEl = e.target as HTMLInputElement
    const file = inputEl.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = async () => {
      try {
        const parsed = JSON.parse(reader.result as string)
        if (!Array.isArray(parsed)) throw new Error('Invalid file')
        const loaded: Message[] = parsed
          .filter((m: any) => m && (m.role === 'user' || m.role === 'assistant') && typeof m.content === 'string')
          .map((m: any) => ({
            id: m.id || 'm' + Date.now() + Math.random().toString(36).slice(2),
            role: m.role,
            content: m.content,
            timestamp: m.timestamp ? new Date(m.timestamp) : new Date(),
            artifacts: Array.isArray(m.artifacts) ? m.artifacts : undefined
          }))
        messages = loaded
        error = null
        await tick()
        scrollToBottom(false)
      } catch {
        error = 'Failed to load history'
      }
    }
    reader.readAsText(file)
    inputEl.value = ''
  }

  async function send() {
    if (!canSend) return
    const text = input.trim()
    input = ''
    isSending = true
    error = null

    const userMsg: Message = {
      id: 'm' + Date.now(),
      role: 'user',
      content: text,
      timestamp: new Date()
    }
    messages = [...messages, userMsg]
    await scrollToBottom()

    const history = messages.map((mm) => ({
      role: mm.role === 'user' ? 'user' : 'assistant',
      content: mm.content
    }))

    try {
      const resp: any = await api.chat.completions({
        model: selectedModel,
        messages: history,
        stream: false
      })
      const raw = resp?.choices?.[0]?.message?.content ?? resp?.choices?.[0]?.text ?? ''
      const { clean, artifacts } = parseCodeArtifacts(raw)
      const assistantMsg: Message = {
        id: 'm' + (Date.now() + 1),
        role: 'assistant',
        content: clean || raw || '(empty response)',
        timestamp: new Date(),
        artifacts: artifacts.length ? artifacts : undefined
      }
      messages = [...messages, assistantMsg]
    } catch (e) {
      const raw = e as any
      if (raw && typeof raw.message === 'string' && raw.message) {
        error = raw.message
      } else if (typeof raw === 'string') {
        error = raw
      } else {
        try { error = JSON.stringify(raw) } catch { error = String(raw) }
        if (!error || error === '{}' || error === '[object Object]') error = 'Request failed'
      }
    } finally {
      isSending = false
      await scrollToBottom()
      textareaEl?.focus()
    }
  }

  async function copyArtifact(code: string, id: string) {
    try {
      await navigator.clipboard.writeText(code)
      copiedArtifact = id
      setTimeout(() => {
        if (copiedArtifact === id) copiedArtifact = null
      }, 1500)
    } catch {}
  }

  function downloadArtifact(code: string, title: string) {
    const blob = new Blob([code], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = title.toLowerCase().replace(/\s+/g, '-') + '.txt'
    a.click()
    URL.revokeObjectURL(url)
  }

  function toggleCollapse(msgId: string, idx: number) {
    messages = messages.map((m) => {
      if (m.id !== msgId || !m.artifacts) return m
      const arts = [...m.artifacts]
      arts[idx] = { ...arts[idx], collapsed: !arts[idx].collapsed }
      return { ...m, artifacts: arts }
    })
  }
</script>

<div class="chat-page">
  <div class="thread" bind:this={threadEl}>
    <div class="thread-inner">
      {#if messages.length === 0}
        <div class="empty-thread">
          <span class="icon empty-icon">chat</span>
          <p class="empty-title">Start a conversation</p>
          <p class="empty-hint">Ask anything — the thread will appear here.</p>
        </div>
      {:else}
        {#each messages as msg (msg.id)}
          <div class="message" class:user={msg.role === 'user'} class:assistant={msg.role === 'assistant'}>
            <div class="meta">
              <span class="sender">{msg.role === 'user' ? 'User' : 'Model'}</span>
              <span class="dot">•</span>
              <span class="time">{formatTime(msg.timestamp)}</span>
            </div>

            {#if msg.role === 'user'}
              <p class="user-text">{msg.content}</p>
            {:else}
              <div class="assistant-text">
                {#each msg.content.split(/(`[^`]+`)/g) as part, i}
                  {#if part.startsWith('`') && part.endsWith('`')}
                    <code class="inline-code">{part.slice(1, -1)}</code>
                  {:else}
                    <span>{part}</span>
                  {/if}
                {/each}
              </div>

              {#if msg.artifacts}
                {#each msg.artifacts as art, idx}
                  <div class="artifact-card" class:collapsed={art.collapsed}>
                    <div class="artifact-header">
                      <div class="artifact-title">
                        <span class="icon code-icon">code</span>
                        <span>{art.title}</span>
                      </div>
                      <div class="artifact-actions">
                        <button class="icon-btn" title="Download" on:click={() => downloadArtifact(art.code, art.title)}>
                          <span class="icon">download</span>
                        </button>
                        <button class="icon-btn" title={copiedArtifact === msg.id + idx ? 'Copied' : 'Copy'} on:click={() => copyArtifact(art.code, msg.id + idx)}>
                          <span class="icon">{copiedArtifact === msg.id + idx ? 'check' : 'content_copy'}</span>
                        </button>
                        <button class="icon-btn" title={art.collapsed ? 'Expand' : 'Collapse'} on:click={() => toggleCollapse(msg.id, idx)}>
                          <span class="icon">{art.collapsed ? 'expand_content' : 'collapse_content'}</span>
                        </button>
                      </div>
                    </div>
                    {#if !art.collapsed}
                      <div class="artifact-body">
                        <pre class="code-pre"><code>{art.code}</code></pre>
                      </div>
                    {/if}
                  </div>
                {/each}
              {/if}
            {/if}
          </div>
        {/each}

        {#if isSending}
          <div class="message assistant">
            <div class="meta">
              <span class="sender">Model</span>
              <span class="dot">•</span>
              <span class="time">now</span>
            </div>
            <div class="typing">
              <span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span>
            </div>
          </div>
        {/if}
        {#if error}
          <div class="error-msg">{error}</div>
        {/if}
      {/if}
    </div>
  </div>

  <div class="composer-dock">
    <div class="composer-card">
      <textarea
        bind:this={textareaEl}
        bind:value={input}
        rows="1"
        placeholder="Start typing a prompt to see what our models can do"
        on:keydown={handleTextareaKeydown}
        on:input={autoResize}
      ></textarea>

      <div class="composer-footer">
        <div class="composer-left">
          <div class="model-picker-wrap">
            <Dropdown
              bind:value={selectedModel}
              options={dropdownOptions}
              placeholder={models.length === 0 ? 'No models' : 'Select model'}
              disabled={models.length === 0}
              autoWidth={true}
              searchable={true}
              rounded="lg"
            />
          </div>
          <ActionDropdown actions={actions} label="Actions" rounded="lg" on:action={handleAction} />
        </div>

        <button class="btn btn-primary run-btn" on:click={send} disabled={!canSend}>
          <span>Run</span><span class="run-arrow">↵</span>
        </button>
      </div>
    </div>
    <div class="composer-hint">Press Enter to send, Shift + Enter for new line</div>
  </div>
</div>

<input bind:this={fileInputEl} type="file" accept="application/json,.json" style="display:none" on:change={handleFileChange} />

<style>
  .chat-page {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    background: var(--color-surface);
  }
  .thread {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 32px 24px 0 24px;
  }
  .thread-inner {
    max-width: 760px;
    width: 100%;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 28px;
    padding-bottom: 16px;
  }
  .empty-thread {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 80px 24px;
    text-align: center;
    color: var(--color-text-soft);
  }
  .empty-icon { font-size: 48px; margin-bottom: 12px; opacity: 0.6; }
  .empty-title { font-size: 16px; font-weight: 500; color: var(--color-text); margin-bottom: 6px; }
  .empty-hint { font-size: 14px; color: var(--color-text-soft); }
  .message { display: flex; flex-direction: column; gap: 8px; }
  .meta { display: flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 500; color: var(--color-text-soft); letter-spacing: 0.01em; }
  .sender { color: var(--color-text-soft); }
  .dot { opacity: 0.5; }
  .time { font-weight: 400; }
  .user-text { font-size: 14px; line-height: 21px; color: var(--color-text); white-space: pre-wrap; word-break: break-word; }
  .assistant-text { font-size: 14px; line-height: 22px; color: var(--color-text); word-break: break-word; }
  .inline-code {
    font-family: 'DM Mono', 'SF Mono', 'Fira Code', monospace;
    font-size: 12.5px;
    background: var(--color-surface-container-highest);
    border: 1px solid var(--color-outline-soft);
    padding: 1px 6px;
    border-radius: 6px;
    color: var(--color-text);
    white-space: nowrap;
  }
  .artifact-card {
    margin-top: 12px;
    border: 1px solid var(--color-outline-light);
    border-radius: 16px;
    background: var(--color-surface-container);
    overflow: hidden;
  }
  :global(.dark) .artifact-card { background: var(--color-surface-container-high); }
  .artifact-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 12px 10px 14px;
    border-bottom: 1px solid var(--color-outline-soft);
    background: var(--color-surface-container-high);
  }
  .artifact-card.collapsed .artifact-header { border-bottom: none; }
  :global(.dark) .artifact-header { background: var(--color-surface-container-highest); }
  .artifact-title { display: flex; align-items: center; gap: 8px; font-size: 13px; font-weight: 500; color: var(--color-text); }
  .code-icon { font-size: 18px; color: var(--color-text-soft); }
  .artifact-actions { display: flex; align-items: center; gap: 2px; }
  .icon-btn {
    width: 28px; height: 28px;
    display: inline-flex; align-items: center; justify-content: center;
    border-radius: 8px; border: 1px solid transparent;
    background: transparent; color: var(--color-text-soft); cursor: pointer;
  }
  .icon-btn:hover { background: var(--color-surface); border-color: var(--color-outline-light); color: var(--color-text); }
  .icon-btn .icon { font-size: 16px; }
  .artifact-body { background: var(--color-surface); overflow-x: auto; }
  .code-pre {
    margin: 0; padding: 16px 20px;
    font-family: 'DM Mono', 'SF Mono', 'Fira Code', monospace;
    font-size: 12.5px; line-height: 19px; color: var(--color-text); white-space: pre;
  }
  .typing { display: flex; gap: 4px; padding: 6px 0; }
  .typing-dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--color-text-soft); opacity: 0.6; animation: typing 1.2s infinite;
  }
  .typing-dot:nth-child(2) { animation-delay: 0.2s; }
  .typing-dot:nth-child(3) { animation-delay: 0.4s; }
  @keyframes typing {
    0%, 100% { opacity: 0.3; transform: translateY(0); }
    50% { opacity: 1; transform: translateY(-2px); }
  }
  .composer-dock {
    flex-shrink: 0;
    padding: 12px 24px 16px 24px;
    background: linear-gradient(to top, var(--color-surface) 85%, transparent);
  }
  .composer-card {
    max-width: 760px; width: 100%; margin: 0 auto;
    background: var(--color-surface-container-high);
    border: 1px solid var(--color-outline-light);
    border-radius: 20px;
    box-shadow: var(--shadow-lg);
    padding: 12px 14px 10px 14px;
    display: flex; flex-direction: column; gap: 10px;
  }
  :global(.dark) .composer-card {
    background: var(--color-surface-container-high);
    border-color: var(--color-outline-soft);
    box-shadow: var(--shadow-xl);
  }
  .composer-card textarea {
    width: 100%; min-height: 24px; max-height: 160px;
    resize: none; border: none; outline: none;
    background: transparent; color: var(--color-text);
    font-family: inherit; font-size: 14px; line-height: 21px;
    padding: 4px 2px; box-shadow: none;
  }
  .composer-card textarea::placeholder { color: var(--color-text-soft); }
  .composer-footer {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
  }
  .composer-left {
    display: flex; align-items: center; gap: 8px;
  }
  .model-picker-wrap { width: auto; flex-shrink: 0; display: inline-flex; }
  .composer-left :global(.dropdown-trigger) { height: 32px; }
  .run-btn { gap: 6px; }
  .run-arrow { margin-left: 2px; font-size: 14px; line-height: 1; }
  .composer-hint {
    max-width: 760px; margin: 8px auto 0 auto;
    text-align: center; font-size: 11px; color: var(--color-text-soft);
  }
  @media (max-width: 640px) {
    .thread { padding: 16px 16px 0 16px; }
    .composer-dock { padding: 8px 12px 12px 12px; }
    .composer-card { border-radius: 16px; }
  }
</style>
