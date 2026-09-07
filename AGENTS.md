# AGENTS.md

Guidelines for AI agents in `llm-router/`. Read before any change. Treat every rule as absolute. No exceptions. No reinterpretation.

## 0. Banned Wording

Apply this section to all agent output: commit messages, PR descriptions, code comments, log strings, UI copy, CLI help/output, error messages, and doc edits.

### 0.1 No Filler Phrases

Rule: NEVER write filler phrases that pad a sentence without adding information.

DON'T:
- "In order to fix this issue, we need to update the router."
- "It's worth noting that the credential pool is LRU-based."
- "Please note that this change also updates the router."
- "As you can see, the test now passes."

DO:
- "Fix the issue by updating the router."
- "The credential pool is LRU-based."
- "Also update the router."
- "The test now passes."

### 0.2 No Implementation-Detail Asides in UI or CLI

Rule: NEVER append a parenthetical (or dash/comma aside) that explains an internal condition, branch, or implementation reason inside UI text, CLI output, log lines, or error messages. State the action or fact only. Put the reasoning in a code comment or commit message, or leave it out entirely — never surface it to the user.

DON'T:
- `"Skip validation... (because SKIP_VALIDATION was true)"`
- `"Load models... (isCacheValid == false)"`
- `"Retry request... (attempt < maxRetries)"`
- `"Save credential... (credential.Token != "")"`

DO:
- `"Skip validation"`
- `"Load models"`
- `"Retry request"`
- `"Save credential"`

If the condition is genuinely useful for debugging, log it at debug level with structured fields (`reason=rate_limited`). Never narrate it in prose inside the user-facing string.

### 0.3 No Unprofessional or Non-Informative Wording

Rule: NEVER use casual, slang, or vague verbs in place of precise technical terms, in code, comments, commits, logs, or docs. Banned examples: "blow up", "wire", "sweep", "spin up", "juice", "nuke", "hack", "magic".

DON'T:
- "This blows up if the token is empty."
- "Wire the new adapter into the router."
- "Sweep stale credentials on startup."
- "Spin up a goroutine to poll the queue."

DO:
- "This panics if the token is empty."
- "Register the new adapter with the router."
- "Remove stale credentials on startup."
- "Start a goroutine to poll the queue."

## 1. Priority Rules

1. NEVER write to `$state` from an `$effect` that reads it. → §5
2. NEVER run, start, build, or live-test the app. Use only the 4 allowed `make` targets. → §4
3. NEVER run destructive git commands. → §6
4. NEVER delete the dev DB. NEVER kill processes. NEVER litter the repo. → §6

## 2. Project

`llm-router` — single-binary OpenAI-compatible LLM routing gateway. Go backend + embedded Svelte SPA + embedded bbolt DB.

- Routes `ModelId = provider/model` (e.g. `opencode-zen/gpt-5`, `agents/my-agent`) → backend + `CredentialPool`.
- Provider backends are single-file Lua plugins (`internal/services/luaplugin/bundled/`, installed store records in `BucketPlugins`). Built-in Go backends exist only for `custom` (OpenAI-compatible passthrough) and `agents` (virtual provider).

## 3. Structure

```
cmd/root.go              CLI entrypoint (--web/--api/--db)
internal/server/         HTTP server: dashboard (8080), /v1 API (8081)
internal/services/
  token/                 router-token issue/validate
  router/                ModelId → backend + Credential, retry engine
  retry/                 single retry/fallthrough engine (router + agents)
  provider/              ProviderInstance CRUD (all types, one path)
  credential/            credential pool, usage stats
  agent/                 agents/* virtual provider
  luaplugin/             Lua execution core: manifest, sandbox, HTTP+SSRF, storage; bundled/*.lua
  pluginrepo/            plugin store: GitHub Contents API + generic index
  modelinfo/             model metadata cache (1h TTL)
  metrics/               1m buckets, 90d retention
  maintenance/           refresh + cleanup
  tokencount/            slop-tokenizer wrapper
internal/repository/     bbolt buckets
internal/dashboard/      admin REST API
internal/api/v1/         OpenAI-compatible /v1/chat/completions, /v1/models
internal/models/         shared wire types
internal/config/         Config struct
internal/adapters/generic/ built-in custom backend (Go)
providers/agents/        built-in agents backend (Go)
web/                     Svelte SPA (web/openapi.yaml + src/lib/generated/ auto-generated — do not hand-edit)
scripts/                 separate Go module — build/dev helpers (never imported by main module)
Makefile                 thin launcher for scripts/ — see §4
```

Rule: keep changes shallow. Do not touch service internals unless the task requires it.

## 4. Commands

### 4.1 Allowed — agent runs these directly

| Command | Purpose |
|---|---|
| `make check-frontend` | Frontend compile/type-check |
| `make go-check` | Go static check (`go vet`) |
| `make go-test` | Go tests |

### 4.2 Banned — raw equivalents of the above

| DO | DON'T |
|---|---|
| `make check-frontend` | `bunx svelte-check`, `bun run check`, `tsc --noEmit`, `eslint .` |
| `make go-check` | `go vet ./...` |
| `make go-test` | `go test ./...` |

Rule: NEVER substitute a raw command for an allowed `make` target, even when the outcome matches.

### 4.3 Banned — every other Makefile target

`start`, `stop`, `restart`, `status`, `browser`, `publish`, `clean`.

NEVER run these. No exception for "just to check", "isolated test", "custom port", or any other framing.

If the task needs one of these: STOP. Ask the human to run it and report back (terminal output, logs, curl/browser result).

| DO | DON'T |
|---|---|
| "Please run `make start` and paste the output." | `go run .`, `go build -o ./llm-router.exe`, `bun run dev`, `Start-Process ...`, any script/wrapper invoking these |

## 5. Svelte 5 Reactivity

Rule: NEVER write to a `$state` variable from an `$effect` that reads it — directly, or via a function it calls. Guard flags and `untrack()` do not fix this — they hide it.

DON'T:
```svelte
$effect(() => {
  if (types.length && !typeKey) {
    untrack(() => { typeKey = types[0] })  // writes typeKey; effect exists because typeKey is read elsewhere → oscillation risk
  }
})
```

DO — computed value → `$derived`:
```svelte
let typeKey = $derived(types[0] ?? '')
```

DO — one-time default on data arrival → imperative, not reactive:
```svelte
onMount(async () => {
  types = await fetchTypes()
  if (types.length) typeKey = types[0]
})
```

DO — parent-notification effect (the only legitimate write-out pattern): list every dependency explicitly, `untrack()` only the outgoing call, never the effect's own state:
```svelte
$effect(() => {
  void a; void b; void c
  untrack(() => notifyParent(a, b, c))
})
```

Rule: never stack a 2nd/3rd `$effect` with its own guard flag to patch the 1st. That means the 1st effect is wrong. Replace it, don't add to it.

Rule: before finishing any `.svelte` change, re-check every `$effect` touched against this section.

## 6. Absolute Prohibitions

| # | Prohibition | Detail |
|---|---|---|
| 1 | Kill processes | No `kill`, `pkill`, `taskkill`, `Stop-Process`. Ask human to run `make stop`. |
| 2 | Delete database files | Never remove `~/.local/llm-router/llm-router-dev.db` (or the Windows equivalent). Ask the human to recreate it via `make start`. |
| 3 | Run destructive git commands | No `git commit`, `git push`, `git reset`, `git checkout -f`, `git clean`, force-push, amend. Commit only if explicitly instructed, for that exact commit only. |
| 4 | Litter the project | No `*.log`, `*.pid`, `*.tmp`, binaries, scratch files, notes inside the project tree. Use `/tmp` or an external scratch dir. |
| 5 | Avoid the Makefile | No raw command substitutes for an allowed target (§4.2). No manual invocation of a banned target's underlying steps (§4.3). |
| 6 | Run, start, or live-test the app | Human-only task. NEVER debug at runtime yourself — ask the human. See §4.3, §8. |
| 7 | Redirect output to nul | A terrible habit that breaks on Windows. |
| 8 | Truncate diagnostics output with tail/head | Diagnostics matter and truncating wastes time. NEVER truncate them. |
| 9 | Fall back silently | NEVER swallow a failure and continue on a fallback path. Surface every failure as an error — return it to the caller, log it, or both — or route it to an explicit, named on-fail branch. Never fall through unannounced. |

## 7. Lua Plugins

- Location: new provider backends are single-file Lua plugins. Develop them anywhere as one `.lua` file with a `--- @` manifest header; install via dashboard Store page or `POST /api/llm-router/dashboard/plugins/install-file`.
- Manifest: required tags `@plugin`, `@author`, `@version`, `@router_version`, one or more `@allow_host` (`*` marks the plugin unsafe). `internal/services/luaplugin/manifest.go` validates.
- API: `llm_router.register(type_key, {complete, ...})`, `llm_router.create_http_client`, `llm_router.storage`, `json.encode/decode`. Error contract `{type=, message=, retry_after=}`. UI trees for `config_schema`/`credential_schema`/`auth_initiate`/`auth_step` render through `DynamicForm.svelte`.
- Bundled plugins live in `internal/services/luaplugin/bundled/*.lua` and install on startup via `EnsureBundled`. Bump `@version` to ship an upgrade.
- Built-in Go backends exist only for `custom` (`internal/adapters/generic/`) and `agents` (`providers/agents/`), both implementing `provider.GoAdapter`.
- Verification: run `make go-check` only. For runtime checks, ask the human to run `make start`.

## 8. When Runtime Info Is Needed

NEVER run the app yourself (§4.3, §6.6). To get runtime facts:

1. State exactly what's needed: endpoint, log line, or behavior.
2. Ask human to run `make start` / `make restart` and report back.
3. Interpret: dev dashboard = `http://HOST:WEB_PORT` proxying `/api/llm-router/*` → backend `HOST:38473` (dev-internal, hardcoded); publish dashboard = `:8080` (embedded). API = `:8081/v1`. Bearer key printed on start and stored at `~/.local/llm-router/llm-router-dev.key`. `401` without that key header is expected, not a bug. Pidfile is JSON `{backend,frontend,vitePort}` at `%TEMP%/llm-router-dev.pid`.
